// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package request provides generic, type-safe request execution helpers and automatic response stream binding.
package request

import (
	"context"
	"errors"
	"net/http"
	"reflect"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
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
//
// When passed as type Resp to generic request helpers like [GetTo], the helper automatically
// drains and closes the response body stream to prevent socket leaks.
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

// Get performs a GET request through c and returns the raw [*http.Response].
func Get(ctx context.Context, c Requester, path string, mods ...aoni.RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodGet, path, mods...)
}

// GetTo performs a GET request and unmarshals the 2xx response payload into a newly allocated Resp instance.
func GetTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	resp, err := c.Request(ctx, http.MethodGet, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// GetInto performs a GET request and unmarshals the response body directly into target, eliminating allocations.
func GetInto[T any](
	ctx context.Context,
	c Requester,
	path string,
	target *T,
	mods ...aoni.RequestModifier,
) error {
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
	var raw *http.Response

	mods = append(mods, mod.WithCaptureResponse(&raw))

	result, err := GetTo[Resp](ctx, c, path, mods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

// GetProtoTo performs a GET request expecting a binary Protocol Buffer stream unmarshaled into Resp.
func GetProtoTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	mods = append([]aoni.RequestModifier{
		mod.WithHeader("Accept", "application/x-protobuf"),
		mod.WithDecoder(decode.ProtoDecoder),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodGet, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// GetProtoInto performs a GET request expecting a binary Protocol Buffer stream unmarshaled directly into target.
func GetProtoInto[T any](
	ctx context.Context,
	c Requester,
	path string,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	mods = append([]aoni.RequestModifier{
		mod.WithHeader("Accept", "application/x-protobuf"),
		mod.WithDecoder(decode.ProtoDecoder),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodGet, path, mods...) //nolint:bodyclose
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
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

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	return c.Request(ctx, http.MethodPost, path, mods...)
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

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
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

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, mods...) //nolint:bodyclose
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
	var raw *http.Response

	mods = append(mods, mod.WithCaptureResponse(&raw))

	result, err := PostTo[Resp](ctx, c, path, body, mods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

// PostProto executes a POST request containing a binary [proto.Message] payload and returns the raw [*http.Response].
func PostProto(
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	mods = append([]aoni.RequestModifier{mod.WithProtoBody(msg)}, mods...)
	return c.Request(ctx, http.MethodPost, path, mods...)
}

// PostProtoTo executes a POST request with a [proto.Message] payload and unmarshals the binary response into Resp.
func PostProtoTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	mods = append([]aoni.RequestModifier{
		mod.WithProtoBody(msg),
		mod.WithDecoder(decode.ProtoDecoder),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PostProtoInto executes a POST request with a [proto.Message] payload and unmarshals the response directly into target.
func PostProtoInto[T any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	mods = append([]aoni.RequestModifier{
		mod.WithProtoBody(msg),
		mod.WithDecoder(decode.ProtoDecoder),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, mods...) //nolint:bodyclose
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// PostGRPCWeb executes a POST request with a gRPC-Web framed [proto.Message] payload and returns the raw [*http.Response].
func PostGRPCWeb(
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	mods = append([]aoni.RequestModifier{mod.WithGRPCWebBody(msg)}, mods...)
	return c.Request(ctx, http.MethodPost, path, mods...)
}

// PostGRPCWebTo executes a POST request with a gRPC-Web framed payload and unmarshals the response into Resp.
func PostGRPCWebTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	mods = append([]aoni.RequestModifier{
		mod.WithGRPCWebBody(msg),
		mod.WithDecoder(decode.GRPCWebDecoder),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// PostGRPCWebInto executes a POST request with a gRPC-Web framed payload and unmarshals the response directly into target.
func PostGRPCWebInto[T any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	mods = append([]aoni.RequestModifier{
		mod.WithGRPCWebBody(msg),
		mod.WithDecoder(decode.GRPCWebDecoder),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPost, path, mods...) //nolint:bodyclose
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// Put executes a PUT request through c and returns the raw [*http.Response].
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

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	return c.Request(ctx, http.MethodPut, path, mods...)
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

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPut, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
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

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPut, path, mods...) //nolint:bodyclose
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
	var raw *http.Response

	mods = append(mods, mod.WithCaptureResponse(&raw))

	result, err := PutTo[Resp](ctx, c, path, body, mods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

// PutProtoTo executes a PUT request carrying a [proto.Message] payload and unmarshals the response into Resp.
func PutProtoTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return DoProtoTo[Resp](ctx, c, http.MethodPut, path, msg, mods...)
}

// PutProtoInto executes a PUT request carrying a [proto.Message] payload and unmarshals the response directly into target.
func PutProtoInto[T any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	return DoProtoInto[T](ctx, c, http.MethodPut, path, msg, target, mods...)
}

// Patch executes a PATCH request through c and returns the raw [*http.Response].
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

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	return c.Request(ctx, http.MethodPatch, path, mods...)
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

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPatch, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
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

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodPatch, path, mods...) //nolint:bodyclose
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
	var raw *http.Response

	mods = append(mods, mod.WithCaptureResponse(&raw))

	result, err := PatchTo[Resp](ctx, c, path, body, mods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

// PatchProtoTo executes a PATCH request with a [proto.Message] payload and unmarshals the response into Resp.
func PatchProtoTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return DoProtoTo[Resp](ctx, c, http.MethodPatch, path, msg, mods...)
}

// PatchProtoInto executes a PATCH request with a [proto.Message] payload and unmarshals the response directly into target.
func PatchProtoInto[T any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	return DoProtoInto[T](ctx, c, http.MethodPatch, path, msg, target, mods...)
}

// Delete executes a DELETE request through c and returns the raw [*http.Response].
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

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	return c.Request(ctx, http.MethodDelete, path, mods...)
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

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodDelete, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
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

	mods = append([]aoni.RequestModifier{
		mod.WithContentType("application/json"),
		mod.WithAccept("application/json"),
		mod.WithBody(bodyReader),
	}, mods...)

	resp, err := c.Request(ctx, http.MethodDelete, path, mods...) //nolint:bodyclose
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
	var raw *http.Response

	mods = append(mods, mod.WithCaptureResponse(&raw))

	result, err := DeleteTo[Resp](ctx, c, path, body, mods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

// DeleteProtoTo executes a DELETE request with an optional [proto.Message] payload and unmarshals the response into Resp.
func DeleteProtoTo[Resp any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	return DoProtoTo[Resp](ctx, c, http.MethodDelete, path, msg, mods...)
}

// DeleteProtoInto executes a DELETE request with an optional [proto.Message] payload and unmarshals the response directly into target.
func DeleteProtoInto[T any](
	ctx context.Context,
	c Requester,
	path string,
	msg proto.Message,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	return DoProtoInto[T](ctx, c, http.MethodDelete, path, msg, target, mods...)
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

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
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

// Connect performs a CONNECT request through c and returns the raw [*http.Response].
func Connect(
	ctx context.Context,
	c Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	return c.Request(ctx, http.MethodConnect, path, mods...)
}

// Do performs an HTTP request using method and optional body, returning the raw [*http.Response].
func Do(
	ctx context.Context,
	c Requester,
	method, path string,
	body any,
	mods ...aoni.RequestModifier,
) (*http.Response, error) {
	if body != nil {
		bodyReader, err := validateAndMarshal(body)
		if err != nil {
			return nil, err
		}

		mods = append([]aoni.RequestModifier{
			mod.WithContentType("application/json"),
			mod.WithAccept("application/json"),
			mod.WithBody(bodyReader),
		}, mods...)
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
	if body != nil {
		bodyReader, err := validateAndMarshal(body)
		if err != nil {
			return nil, err
		}

		mods = append([]aoni.RequestModifier{
			mod.WithContentType("application/json"),
			mod.WithAccept("application/json"),
			mod.WithBody(bodyReader),
		}, mods...)
	}

	resp, err := c.Request(ctx, method, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
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
	if body != nil {
		bodyReader, err := validateAndMarshal(body)
		if err != nil {
			return err
		}

		mods = append([]aoni.RequestModifier{
			mod.WithContentType("application/json"),
			mod.WithAccept("application/json"),
			mod.WithBody(bodyReader),
		}, mods...)
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
	var raw *http.Response

	mods = append(mods, mod.WithCaptureResponse(&raw))

	result, err := DoTo[Resp](ctx, c, method, path, body, mods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

// DoProtoTo executes an HTTP request with a [proto.Message] payload and unmarshals the response into Resp.
func DoProtoTo[Resp any](
	ctx context.Context,
	c Requester,
	method, path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	if msg != nil {
		mods = append([]aoni.RequestModifier{mod.WithProtoBody(msg)}, mods...)
	} else {
		mods = append([]aoni.RequestModifier{mod.WithHeader("Accept", "application/x-protobuf")}, mods...)
	}

	mods = append(mods, mod.WithDecoder(decode.ProtoDecoder))

	resp, err := c.Request(ctx, method, path, mods...) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	if reflect.TypeFor[Resp]() == reflect.TypeFor[NoResponse]() {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

// DoProtoInto executes an HTTP request with a [proto.Message] payload and unmarshals the response directly into target.
func DoProtoInto[T any](
	ctx context.Context,
	c Requester,
	method, path string,
	msg proto.Message,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	if msg != nil {
		mods = append([]aoni.RequestModifier{mod.WithProtoBody(msg)}, mods...)
	} else {
		mods = append([]aoni.RequestModifier{mod.WithHeader("Accept", "application/x-protobuf")}, mods...)
	}

	mods = append(mods, mod.WithDecoder(decode.ProtoDecoder))

	resp, err := c.Request(ctx, method, path, mods...) //nolint:bodyclose
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}
