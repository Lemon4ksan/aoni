// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package request provides generic, type-safe request execution helpers and automatic response stream binding.
package request

import (
	"context"
	"errors"
	stdio "io"
	"net/http"
	"reflect"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
)

var (
	// ErrUnexpectedContentType indicates that the response Content-Type header violates expected structured MIME formats.
	ErrUnexpectedContentType = errors.New("aoni: unexpected content-type (possible captive portal or intercept)")

	// ErrModifierAsBody is returned when an [aoni.RequestModifier] is accidentally passed as a request payload argument.
	ErrModifierAsBody = errors.New("aoni: passed a RequestModifier as the request body payload")

	// ErrNilResponse is returned when attempting to process a nil [*http.Response].
	ErrNilResponse = errors.New("aoni: response is nil")
)

// DefaultClient provides a shared default [aoni.Client] instance used by global request helpers.
var DefaultClient = aoni.NewClient(nil)

// NoResponse is a sentinel type indicating a request that produces no unmarshaled body structure.
type NoResponse struct{}

// Unwrapper allows nested decorator wrappers to be unwrapped down to the root [Requester].
type Unwrapper interface {
	Unwrap() Requester
}

// UnwrapClient peels away all [Unwrapper] decorator layers from r and returns the innermost [*aoni.Client].
func UnwrapClient(r Requester) (c *aoni.Client) {
	for {
		if client, ok := r.(*aoni.Client); ok {
			return client
		}

		u, ok := r.(Unwrapper)
		if !ok {
			break
		}

		r = u.Unwrap()
	}

	return nil
}

// Requester specifies the core client contract capable of executing parameterized HTTP requests.
type Requester interface {
	Request(
		ctx context.Context,
		method, path string,
		mods ...aoni.RequestModifier,
	) (*http.Response, error)
}

// Subscriber defines the universal contract for subscribing to inbound binary events.
type Subscriber interface {
	// Subscribe binds an event receiver for the specified event ID and returns an unsubscribe cleanup func.
	Subscribe(eventID any, handler func(raw []byte)) (unsubscribe func())
}

// Transport defines the universal low-level wire contract for RPC, notifications, and event streams.
type Transport interface {
	Subscriber

	// Invoke executes a request-response RPC over the wire.
	Invoke(ctx context.Context, op any, payload []byte) (response []byte, err error)

	// Notify sends a one-way message without waiting for a reply.
	Notify(ctx context.Context, op any, payload []byte) error
}

// AsRequester adapts any execution engine, client, or [aoni.RequestDoer] into a [Requester].
func AsRequester(doer any) Requester {
	if doer == nil {
		return DefaultClient
	}

	if r, ok := doer.(Requester); ok {
		return r
	}

	if rd, ok := doer.(aoni.RequestDoer); ok {
		return aoni.NewClient(rd)
	}

	return aoni.NewClient(doer)
}

// Configure applies [aoni.ClientOption] layers to any client or engine, returning a configured [Requester].
func Configure(doer any, opts ...aoni.ClientOption) Requester {
	return AsRequester(aoni.Configure(doer, opts...))
}

// BindRequester adapts any doer into a [Requester] and applies initial [aoni.ClientOption] layers.
func BindRequester(doer any, opts ...aoni.ClientOption) Requester {
	r := AsRequester(doer)
	if len(opts) > 0 {
		return Configure(r, opts...)
	}

	return r
}

// Get performs a GET request through c and returns the raw [*http.Response].
func Get(ctx context.Context, c Requester, path string, mods ...aoni.RequestModifier) (*http.Response, error) {
	if c == nil {
		c = DefaultClient
	}

	return c.Request(ctx, http.MethodGet, path, mods...)
}

// GetTo performs a GET request and unmarshals the 2xx response payload into a newly allocated Resp instance.
func GetTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	if c == nil {
		c = DefaultClient
	}

	resp, err := c.Request(ctx, http.MethodGet, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](resp, c)
}

// GetInto performs a GET request and unmarshals the response body directly into target, eliminating allocations.
func GetInto[T any](
	ctx context.Context,
	c Requester,
	path string,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	if c == nil {
		c = DefaultClient
	}

	resp, err := c.Request(ctx, http.MethodGet, path, mods...) //nolint:bodyclose
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// GetToEx is like [GetTo], but yields both the unmarshaled payload structure and the raw [*http.Response].
func GetToEx[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodGet, path, nil, mods)
}

// Post executes a POST request carrying body marshaled as JSON and returns the raw [*http.Response].
func Post(
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	return c.Request(ctx, http.MethodPost, path, allMods...)
}

// PostTo executes a POST request with body payload and unmarshals the response into Resp.
func PostTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	resp, err := c.Request(ctx, http.MethodPost, path, allMods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](resp, c)
}

// PostInto executes a POST request with body payload and unmarshals the response directly into target.
func PostInto[T any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return err
	}

	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	resp, err := c.Request(ctx, http.MethodPost, path, allMods...) //nolint:bodyclose
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// PostToEx is like [PostTo], but yields both the unmarshaled payload structure and the raw [*http.Response].
func PostToEx[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodPost, path, body, mods)
}

// Put performs a PUT request carrying body marshaled as JSON and returns the raw [*http.Response].
func Put(
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	return c.Request(ctx, http.MethodPut, path, allMods...)
}

// PutTo executes a PUT request and unmarshals the response payload into Resp.
func PutTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	resp, err := c.Request(ctx, http.MethodPut, path, allMods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](resp, c)
}

// PutInto executes a PUT request and unmarshals the response payload directly into target.
func PutInto[T any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return err
	}

	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	resp, err := c.Request(ctx, http.MethodPut, path, allMods...) //nolint:bodyclose
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// PutToEx is like [PutTo], but yields both the unmarshaled payload structure and the raw [*http.Response].
func PutToEx[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodPut, path, body, mods)
}

// Patch performs a PATCH request carrying body marshaled as JSON and returns the raw [*http.Response].
func Patch(
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	return c.Request(ctx, http.MethodPatch, path, allMods...)
}

// PatchTo executes a PATCH request and unmarshals the response payload into Resp.
func PatchTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	resp, err := c.Request(ctx, http.MethodPatch, path, allMods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](resp, c)
}

// PatchInto executes a PATCH request and unmarshals the response payload directly into target.
func PatchInto[T any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return err
	}

	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	resp, err := c.Request(ctx, http.MethodPatch, path, allMods...) //nolint:bodyclose
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// PatchToEx is like [PatchTo], but yields both the unmarshaled payload structure and the raw [*http.Response].
func PatchToEx[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodPatch, path, body, mods)
}

// Delete performs a DELETE request carrying body marshaled as JSON and returns the raw [*http.Response].
func Delete(
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	return c.Request(ctx, http.MethodDelete, path, allMods...)
}

// DeleteTo executes a DELETE request and unmarshals the response payload into Resp.
func DeleteTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return nil, err
	}

	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	resp, err := c.Request(ctx, http.MethodDelete, path, allMods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](resp, c)
}

// DeleteInto executes a DELETE request and unmarshals the response payload directly into target.
func DeleteInto[T any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	bodyReader, err := validateAndMarshal(body)
	if err != nil {
		return err
	}

	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withJSONBodyMods(&stackBuf, bodyReader, mods)

	resp, err := c.Request(ctx, http.MethodDelete, path, allMods...) //nolint:bodyclose
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// DeleteToEx is like [DeleteTo], but yields both the unmarshaled payload structure and the raw [*http.Response].
func DeleteToEx[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodDelete, path, body, mods)
}

// Head performs a HEAD request through c and returns the raw [*http.Response].
func Head(
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	return c.Request(ctx, http.MethodHead, path, mods...)
}

// Options performs an OPTIONS request through c and returns the raw [*http.Response].
func Options(
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	return c.Request(ctx, http.MethodOptions, path, mods...)
}

// OptionsTo performs an OPTIONS request and unmarshals the response payload into Resp.
func OptionsTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	resp, err := c.Request(ctx, http.MethodOptions, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](resp, c)
}

// OptionsInto performs an OPTIONS request and unmarshals the response payload directly into target.
func OptionsInto[T any](
	ctx context.Context,
	c Requester,
	path string,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	resp, err := c.Request(ctx, http.MethodOptions, path, mods...) //nolint:bodyclose
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// Trace performs a TRACE request through c and returns the raw [*http.Response].
func Trace(
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	return c.Request(ctx, http.MethodTrace, path, mods...)
}

// Connect performs a CONNECT tunnel handshake through c and returns the raw [*http.Response].
func Connect(
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	return c.Request(ctx, http.MethodConnect, path, mods...)
}

// Fetch performs an HTTP request using method and unmarshals the response body directly into target.
func Fetch[T any](
	ctx context.Context,
	c Requester,
	method, path string,
	body any,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	return DoInto(ctx, c, method, path, body, target, mods...)
}

// FetchTo executes an HTTP request using method and unmarshals the response payload into Resp.
func FetchTo[Resp any](
	ctx context.Context,
	c Requester,
	method, path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return DoTo[Resp](ctx, c, method, path, body, mods...)
}

// Do performs an HTTP request using method and returns the raw [*http.Response].
func Do(
	ctx context.Context,
	c Requester,
	method, path string,
	body any,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	if c == nil {
		c = DefaultClient
	}

	if body != nil {
		bodyReader, err := validateAndMarshal(body)
		if err != nil {
			return nil, err
		}

		var stackBuf [stackModCapacity]aoni.RequestModifier

		mods = withJSONBodyMods(&stackBuf, bodyReader, mods)
	}

	return c.Request(ctx, method, path, mods...)
}

// DoTo performs an HTTP request using method, marshaling body if provided, and unmarshals response into Resp.
func DoTo[Resp any](
	ctx context.Context,
	c Requester,
	method, path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	if c == nil {
		c = DefaultClient
	}

	if body != nil {
		bodyReader, err := validateAndMarshal(body)
		if err != nil {
			return nil, err
		}

		var stackBuf [stackModCapacity]aoni.RequestModifier

		mods = withJSONBodyMods(&stackBuf, bodyReader, mods)
	}

	resp, err := c.Request(ctx, method, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](resp, c)
}

// DoInto performs an HTTP request using method, marshaling body if provided, and unmarshals response directly into target.
func DoInto[T any](
	ctx context.Context,
	c Requester,
	method, path string,
	body any,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	if c == nil {
		c = DefaultClient
	}

	if body != nil {
		bodyReader, err := validateAndMarshal(body)
		if err != nil {
			return err
		}

		var stackBuf [stackModCapacity]aoni.RequestModifier

		mods = withJSONBodyMods(&stackBuf, bodyReader, mods)
	}

	resp, err := c.Request(ctx, method, path, mods...) //nolint:bodyclose
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// DoToEx is like [DoTo], but yields both the unmarshaled payload structure and the raw [*http.Response].
func DoToEx[Resp any](
	ctx context.Context,
	c Requester,
	method, path string,
	body any,
	mods ...aoni.RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, method, path, body, mods)
}

// decodeResponseTo unmarshals resp payload into a newly allocated instance of Resp.
func decodeResponseTo[Resp any](resp *http.Response, c Requester) (*Resp, error) {
	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// executeToEx executes a generic typed request while capturing the raw *http.Response.
func executeToEx[Resp any](
	ctx context.Context,
	c Requester,
	method, path string,
	body any,
	mods []aoni.RequestModifier,
) (*Resp, *http.Response, error) {
	var raw *http.Response

	var stackBuf [stackModCapacity]aoni.RequestModifier

	reqMods := withCaptureMod(&stackBuf, &raw, mods)

	result, err := DoTo[Resp](ctx, c, method, path, body, reqMods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

const stackModCapacity = 16

func withJSONBodyMods(
	stackBuf *[stackModCapacity]aoni.RequestModifier,
	bodyReader stdio.Reader,
	mods []aoni.RequestModifier,
) []aoni.RequestModifier {
	total := 3 + len(mods)
	if total <= stackModCapacity {
		stackBuf[0] = mod.WithBody(bodyReader)
		stackBuf[1] = mod.WithContentType("application/json")
		stackBuf[2] = mod.WithAccept("application/json")
		copy(stackBuf[3:], mods)

		return stackBuf[:total]
	}

	res := make([]aoni.RequestModifier, total)
	res[0] = mod.WithBody(bodyReader)
	res[1] = mod.WithContentType("application/json")
	res[2] = mod.WithAccept("application/json")
	copy(res[3:], mods)

	return res
}

func withCaptureMod(
	stackBuf *[stackModCapacity]aoni.RequestModifier,
	raw **http.Response,
	mods []aoni.RequestModifier,
) []aoni.RequestModifier {
	total := 1 + len(mods)
	if total <= stackModCapacity {
		stackBuf[0] = mod.WithCaptureResponse(raw)
		copy(stackBuf[1:], mods)

		return stackBuf[:total]
	}

	res := make([]aoni.RequestModifier, total)
	res[0] = mod.WithCaptureResponse(raw)
	copy(res[1:], mods)

	return res
}

// GetResult dispatches a GET request and returns a Swift-inspired [generic.Result] wrapping the response.
func GetResult[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (generic.Result[*Resp], *http.Response) {
	val, resp, err := GetToEx[Resp](ctx, c, path, mods...)
	if err != nil {
		return generic.Failure[*Resp](err), resp
	}

	return generic.Success(val), resp
}

// PostResult dispatches a JSON POST request and returns a Swift-inspired [generic.Result].
func PostResult[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (generic.Result[*Resp], *http.Response) {
	val, resp, err := PostToEx[Resp](ctx, c, path, body, mods...)
	if err != nil {
		return generic.Failure[*Resp](err), resp
	}

	return generic.Success(val), resp
}

// PutResult dispatches a JSON PUT request and returns a Swift-inspired [generic.Result].
func PutResult[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (generic.Result[*Resp], *http.Response) {
	val, resp, err := PutToEx[Resp](ctx, c, path, body, mods...)
	if err != nil {
		return generic.Failure[*Resp](err), resp
	}

	return generic.Success(val), resp
}

// DeleteResult dispatches a DELETE request and returns a Swift-inspired [generic.Result].
func DeleteResult[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (generic.Result[*Resp], *http.Response) {
	val, resp, err := DeleteToEx[Resp](ctx, c, path, body, mods...)
	if err != nil {
		return generic.Failure[*Resp](err), resp
	}

	return generic.Success(val), resp
}

// PatchResult dispatches a JSON PATCH request and returns a Swift-inspired [generic.Result].
func PatchResult[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (generic.Result[*Resp], *http.Response) {
	val, resp, err := PatchToEx[Resp](ctx, c, path, body, mods...)
	if err != nil {
		return generic.Failure[*Resp](err), resp
	}

	return generic.Success(val), resp
}

type raceResult[Resp any] struct {
	val  *Resp
	resp *http.Response
}

// RaceGet concurrently dispatches GET requests to multiple target paths/URLs through c,
// returning the result of the first endpoint that succeeds.
//
// All other competing requests are immediately cancelled via context.
func RaceGet[Resp any](
	ctx context.Context,
	c Requester,
	paths ...string,
) (generic.Result[*Resp], *http.Response) {
	if len(paths) == 0 {
		return generic.Failure[*Resp](errors.New("aoni: no paths provided for RaceGet")), nil
	}

	tasks := make([]func(context.Context) generic.Result[raceResult[Resp]], len(paths))
	for i, p := range paths {
		targetPath := p
		tasks[i] = func(reqCtx context.Context) generic.Result[raceResult[Resp]] {
			val, resp, err := GetToEx[Resp](reqCtx, c, targetPath) //nolint:bodyclose
			if err != nil {
				return generic.Failure[raceResult[Resp]](err)
			}

			return generic.Success(raceResult[Resp]{val: val, resp: resp})
		}
	}

	res := generic.RaceFirstSuccess(ctx, tasks...)
	if !res.IsSuccess() {
		_, err := res.Unwrap()
		return generic.Failure[*Resp](err), nil
	}

	rr, _ := res.Unwrap()

	return generic.Success(rr.val), rr.resp
}
