// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bufio"
	"context"
	"net/http"

	"github.com/lemon4ksan/foundation/borrow"
	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/internal/body"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/response"
	"github.com/lemon4ksan/aoni/mod"
)

// NoResponse is a sentinel type indicating a request that produces no unmarshaled body structure.
type NoResponse = core.NoResponse

var (
	// ErrUnexpectedContentType indicates that the response Content-Type header violates expected structured MIME formats.
	ErrUnexpectedContentType = core.ErrUnexpectedContentType

	// ErrModifierAsBody is returned when a [RequestModifier] is accidentally passed as a request body payload argument.
	ErrModifierAsBody = core.ErrModifierAsBody

	// ErrNilResponse is returned when attempting to process a nil [*http.Response].
	ErrNilResponse = core.ErrNilResponse
)

const stackModCap = 16

// --- Generic Typed HTTP Methods on *Client ---

// GetTo executes an HTTP GET request and automatically decodes the response body into type Resp.
//
// Selects the optimal unmarshaling strategy based on the response Content-Type (JSON, XML, or Protobuf).
// On non-2xx HTTP responses, returns an [*APIError] containing the status code, response headers, and error body.
//
// # Resource Management
//
// The underlying response body stream is automatically drained and closed. Callers do NOT need to call Body.Close().
//
// # Example: Simple Typed Fetch
//
//	type User struct {
//	    ID   int    `json:"id"`
//	    Name string `json:"name"`
//	}
//
//	user, err := client.GetTo[User](ctx, "/users/42")
//	if err != nil {
//	    if aoni.IsNotFound(err) {
//	        // Handle HTTP 404
//	    }
//	    return err
//	}
//
// # Example: Modifiers & Authentication
//
//	user, err := client.GetTo[User](ctx, "/me",
//	    mod.WithBearer(token),
//	    mod.WithQuery("fields", "id,name,email"),
//	)
func (c *Client) GetTo[Resp any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*Resp, error) {
	//nolint:bodyclose // body is closed inside decodeResponseTo
	resp, err := c.Request(ctx, http.MethodGet, path, mods...)
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](c, resp)
}

// GetInto executes an HTTP GET request and decodes the response directly into target without heap allocations.
func (c *Client) GetInto[Resp any](
	ctx context.Context,
	path string,
	target *Resp,
	mods ...RequestModifier,
) error {
	//nolint:bodyclose // body is closed inside HandleResponse
	resp, err := c.Request(ctx, http.MethodGet, path, mods...)
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// GetEx executes an HTTP GET request and returns both the unmarshaled *Resp and the raw [*http.Response] metadata.
func (c *Client) GetEx[Resp any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodGet, path, nil, mods)
}

// PostTo executes an HTTP POST request carrying body and unmarshals the response into *Resp.
func (c *Client) PostTo[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	return c.FetchTo[Resp](ctx, http.MethodPost, path, body, mods...)
}

// PostInto executes an HTTP POST request carrying body and unmarshals the response payload directly into target.
func (c *Client) PostInto[Resp any](
	ctx context.Context,
	path string,
	body any,
	target *Resp,
	mods ...RequestModifier,
) error {
	return c.FetchInto(ctx, http.MethodPost, path, body, target, mods...)
}

// PostEx executes an HTTP POST request carrying body and returns both the unmarshaled *Resp and raw [*http.Response].
func (c *Client) PostEx[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodPost, path, body, mods)
}

// PutTo executes an HTTP PUT request carrying body and unmarshals the response into *Resp.
//
// See [Client.PostTo] for body serialization rules and [Client.GetTo] for automatic response decoding and resource management.
func (c *Client) PutTo[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	return c.FetchTo[Resp](ctx, http.MethodPut, path, body, mods...)
}

// PutInto executes an HTTP PUT request carrying body and unmarshals the response payload directly into target without allocations.
//
// See [Client.PostInto] for body serialization rules and [Client.GetInto] for target decoding.
func (c *Client) PutInto[Resp any](
	ctx context.Context,
	path string,
	body any,
	target *Resp,
	mods ...RequestModifier,
) error {
	return c.FetchInto(ctx, http.MethodPut, path, body, target, mods...)
}

// PutEx executes an HTTP PUT request carrying body and returns both the unmarshaled *Resp and raw [*http.Response].
//
// See [Client.PostEx] for details.
func (c *Client) PutEx[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodPut, path, body, mods)
}

// PatchTo executes an HTTP PATCH request carrying body and unmarshals the response into *Resp.
//
// See [Client.PostTo] for body serialization rules and [Client.GetTo] for automatic response decoding and resource management.
func (c *Client) PatchTo[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	return c.FetchTo[Resp](ctx, http.MethodPatch, path, body, mods...)
}

// PatchInto executes an HTTP PATCH request carrying body and unmarshals the response payload directly into target without allocations.
//
// See [Client.PostInto] for body serialization rules and [Client.GetInto] for target decoding.
func (c *Client) PatchInto[Resp any](
	ctx context.Context,
	path string,
	body any,
	target *Resp,
	mods ...RequestModifier,
) error {
	return c.FetchInto(ctx, http.MethodPatch, path, body, target, mods...)
}

// PatchEx executes an HTTP PATCH request carrying body and returns both the unmarshaled *Resp and raw [*http.Response].
//
// See [Client.PostEx] for details.
func (c *Client) PatchEx[Resp any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodPatch, path, body, mods)
}

// DeleteTo executes an HTTP DELETE request and unmarshals any returned response payload into *Resp.
//
// See [Client.GetTo] for automatic response decoding and resource management.
func (c *Client) DeleteTo[Resp any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*Resp, error) {
	//nolint:bodyclose // body is closed inside decodeResponseTo
	resp, err := c.Request(ctx, http.MethodDelete, path, mods...)
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](c, resp)
}

// DeleteInto executes an HTTP DELETE request and unmarshals the response directly into target without allocations.
//
// See [Client.GetInto] for target decoding and error handling.
func (c *Client) DeleteInto[Resp any](
	ctx context.Context,
	path string,
	target *Resp,
	mods ...RequestModifier,
) error {
	//nolint:bodyclose // body is closed inside HandleResponse
	resp, err := c.Request(ctx, http.MethodDelete, path, mods...)
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// DeleteEx executes an HTTP DELETE request and returns both the unmarshaled *Resp and raw [*http.Response].
//
// See [Client.GetEx] for details.
func (c *Client) DeleteEx[Resp any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, http.MethodDelete, path, nil, mods)
}

// FetchTo executes an arbitrary HTTP method request, marshaling body if provided, and unmarshals the response into *Resp.
func (c *Client) FetchTo[Resp any](
	ctx context.Context,
	method, path string,
	body any,
	mods ...RequestModifier,
) (*Resp, error) {
	var stackBuf [stackModCap]RequestModifier

	allMods, err := prepareBodyMods(body, mods, &stackBuf)
	if err != nil {
		return nil, err
	}

	//nolint:bodyclose // body is closed inside decodeResponseTo
	resp, err := c.Request(ctx, method, path, allMods...)
	if err != nil {
		return nil, err
	}

	return decodeResponseTo[Resp](c, resp)
}

// FetchInto executes an arbitrary HTTP method request, marshaling body if provided, and unmarshals response into target.
func (c *Client) FetchInto[Resp any](
	ctx context.Context,
	method, path string,
	body any,
	target *Resp,
	mods ...RequestModifier,
) error {
	var stackBuf [stackModCap]RequestModifier

	allMods, err := prepareBodyMods(body, mods, &stackBuf)
	if err != nil {
		return err
	}

	//nolint:bodyclose // body is closed inside HandleResponse
	resp, err := c.Request(ctx, method, path, allMods...)
	if err != nil {
		return err
	}

	return HandleResponse(resp, target, c)
}

// DoInto is an alias for [Client.FetchInto].
func (c *Client) DoInto[Resp any](
	ctx context.Context,
	method, path string,
	body any,
	target *Resp,
	mods ...RequestModifier,
) error {
	return c.FetchInto(ctx, method, path, body, target, mods...)
}

// FetchEx performs an arbitrary HTTP request and returns both the unmarshaled *Resp and raw [*http.Response].
func (c *Client) FetchEx[Resp any](
	ctx context.Context,
	method, path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return executeToEx[Resp](ctx, c, method, path, body, mods)
}

// DoEx is an alias for [Client.FetchEx].
func (c *Client) DoEx[Resp any](
	ctx context.Context,
	method, path string,
	body any,
	mods ...RequestModifier,
) (*Resp, *http.Response, error) {
	return c.FetchEx[Resp](ctx, method, path, body, mods...)
}

// FetchTo executes a request with method, path, and optional modifiers, unmarshaling the 2xx response into T.
func FetchTo[T any](
	ctx context.Context,
	c any,
	method, path string,
	mods ...RequestModifier,
) (T, error) {
	var (
		target T
		doer   HTTPRequester
	)

	if d, ok := c.(HTTPRequester); ok {
		doer = d
	} else if c == nil {
		doer = DefaultClient
	}

	resp, err := acquireRequestBuilder(doer).
		SetContext(ctx).
		SetResult(&target).
		Apply(mods...).
		Execute(method, path)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	return target, err
}

// FetchScoped executes a request with method, path, and optional modifiers, passing the decoded response
// into fn within an active [borrow.Scope].
func FetchScoped[T any](
	ctx context.Context,
	c any,
	method, path string,
	fn func(scope *borrow.Scope, val T, resp *http.Response) error,
	mods ...RequestModifier,
) error {
	var (
		target T
		doer   HTTPRequester
	)

	if d, ok := c.(HTTPRequester); ok {
		doer = d
	} else if c == nil {
		doer = DefaultClient
	}

	resp, err := acquireRequestBuilder(doer).
		SetContext(ctx).
		SetResult(&target).
		Apply(mods...).
		Execute(method, path)
	if err != nil {
		return err
	}

	if resp != nil && resp.Body != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
	}

	scope := borrow.AcquireScope()
	defer scope.Release()

	return fn(scope, target, resp)
}

// BatchFetchTo dispatches multiple requests concurrently and unmarshals each 2xx response payload into a slice of T.
func BatchFetchTo[T any](
	ctx context.Context,
	c any,
	method string,
	paths []string,
	mods ...RequestModifier,
) ([]T, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	results := make([]T, len(paths))

	type fetchResult struct {
		idx int
		val T
		err error
	}

	resCh := make(chan fetchResult, len(paths))

	for i, path := range paths {
		go func(idx int, p string) {
			val, err := FetchTo[T](ctx, c, method, p, mods...)
			resCh <- fetchResult{idx: idx, val: val, err: err}
		}(i, path)
	}

	var firstErr error
	for range paths {
		res := <-resCh
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		} else if res.err == nil {
			results[res.idx] = res.val
		}
	}

	return results, firstErr
}

// FetchEither executes an HTTP request, decoding:
//   - 2xx response bodies into type R (Right)
//   - 4xx/5xx response bodies into type L (Left)
//
// Network failures, connection timeouts, or decoding errors are returned as error.
// The response body stream is automatically drained and closed.
func FetchEither[L, R any](
	ctx context.Context,
	c any,
	method, path string,
	body any,
	mods ...RequestModifier,
) (generic.Either[L, R], *http.Response, error) {
	var doer HTTPRequester
	if d, ok := c.(HTTPRequester); ok {
		doer = d
	} else if c == nil {
		doer = DefaultClient
	}

	var (
		right R
		left  L
	)

	b := acquireRequestBuilder(doer).
		SetContext(ctx).
		SetResult(&right).
		SetError(&left)

	if body != nil {
		b.SetBody(body)
	}

	b.Apply(mods...)

	resp, err := b.Execute(method, path)
	if err != nil {
		if resp != nil && resp.StatusCode >= http.StatusBadRequest {
			return generic.Left[L, R](left), resp, nil
		}

		return generic.Either[L, R]{}, resp, err
	}

	return generic.Right[L](right), resp, nil
}

// FetchEither executes an HTTP request and decodes 2xx responses into type R (Right)
// and 4xx/5xx responses into type L (Left).
func (c *Client) FetchEither[L, R any](
	ctx context.Context,
	method, path string,
	body any,
	mods ...RequestModifier,
) (generic.Either[L, R], *http.Response, error) {
	return FetchEither[L, R](ctx, c, method, path, body, mods...)
}

// GetEither executes an HTTP GET request, decoding 2xx responses into type R (Right)
// and 4xx/5xx responses into type L (Left).
func (c *Client) GetEither[L, R any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (generic.Either[L, R], *http.Response, error) {
	return c.FetchEither[L, R](ctx, http.MethodGet, path, nil, mods...)
}

// PostEither executes an HTTP POST request, decoding 2xx responses into type R (Right)
// and 4xx/5xx responses into type L (Left).
func (c *Client) PostEither[L, R any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (generic.Either[L, R], *http.Response, error) {
	return c.FetchEither[L, R](ctx, http.MethodPost, path, body, mods...)
}

// GetResult executes an HTTP GET request and returns a pure [generic.Result] wrapping the decoded response body.
// The response body stream is automatically drained and closed.
func (c *Client) GetResult[T any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) generic.Result[T] {
	val, err := c.GetTo[T](ctx, path, mods...)
	if err != nil {
		return generic.Failure[T](err)
	}

	return generic.Success(generic.Deref(val))
}

// PostResult executes an HTTP POST request and returns a pure [generic.Result] wrapping the decoded response body.
// The response body stream is automatically drained and closed.
func (c *Client) PostResult[T any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) generic.Result[T] {
	val, err := c.PostTo[T](ctx, path, body, mods...)
	if err != nil {
		return generic.Failure[T](err)
	}

	return generic.Success(generic.Deref(val))
}

// FetchResult executes an HTTP request and returns a pure [generic.Result] wrapping the decoded response body.
// The response body stream is automatically drained and closed.
func (c *Client) FetchResult[T any](
	ctx context.Context,
	method, path string,
	body any,
	mods ...RequestModifier,
) generic.Result[T] {
	//nolint:bodyclose // Body is drained and closed in pipeline.
	val, _, err := c.FetchEx[T](ctx, method, path, body, mods...)
	if err != nil {
		return generic.Failure[T](err)
	}

	return generic.Success(generic.Deref(val))
}

// GetEither executes an HTTP GET request on [DefaultClient], decoding 2xx responses into type R (Right)
// and 4xx/5xx responses into type L (Left).
func GetEither[L, R any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) (generic.Either[L, R], *http.Response, error) {
	return DefaultClient.GetEither[L, R](ctx, path, mods...)
}

// PostEither executes an HTTP POST request on [DefaultClient], decoding 2xx responses into type R (Right)
// and 4xx/5xx responses into type L (Left).
func PostEither[L, R any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) (generic.Either[L, R], *http.Response, error) {
	return DefaultClient.PostEither[L, R](ctx, path, body, mods...)
}

// GetResult executes an HTTP GET request on [DefaultClient] and returns a pure [generic.Result].
func GetResult[T any](
	ctx context.Context,
	path string,
	mods ...RequestModifier,
) generic.Result[T] {
	return DefaultClient.GetResult[T](ctx, path, mods...)
}

// PostResult executes an HTTP POST request on [DefaultClient] and returns a pure [generic.Result].
func PostResult[T any](
	ctx context.Context,
	path string,
	body any,
	mods ...RequestModifier,
) generic.Result[T] {
	return DefaultClient.PostResult[T](ctx, path, body, mods...)
}

// HandleResponse processes and decodes an HTTP response stream into a target structure or API error.
func HandleResponse(resp *http.Response, target, client any) error {
	return response.Handle(resp, target, client)
}

// ResolvePeekableReader returns a peekable reader for the response body.
func ResolvePeekableReader(resp *http.Response) *bufio.Reader {
	return response.ResolvePeekableReader(resp)
}

// --- Internal Decoding & Response Handlers ---

func decodeResponseTo[Resp any](c *Client, resp *http.Response) (*Resp, error) {
	var zero *Resp
	if _, ok := any(zero).(*NoResponse); ok {
		return nil, HandleResponse(resp, nil, c)
	}

	result := new(Resp)
	if err := HandleResponse(resp, result, c); err != nil {
		return nil, err
	}

	return result, nil
}

func executeToEx[Resp any](
	ctx context.Context,
	c *Client,
	method, path string,
	body any,
	mods []RequestModifier,
) (*Resp, *http.Response, error) {
	var (
		raw      *http.Response
		stackBuf [stackModCap]RequestModifier
	)

	reqMods := withCaptureMod(&stackBuf, &raw, mods)

	result, err := c.FetchTo[Resp](ctx, method, path, body, reqMods...)
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close()
		}

		return nil, raw, err
	}

	return result, raw, nil
}

func prepareBodyMods(
	bodyInput any,
	mods []RequestModifier,
	stackBuf *[stackModCap]RequestModifier,
) ([]RequestModifier, error) {
	if bodyInput == nil {
		return mods, nil
	}

	payload, err := body.ValidateAndMarshal(bodyInput)
	if err != nil {
		return nil, err
	}

	if payload.IsEmpty() {
		return mods, nil
	}

	totalLen := len(mods) + generic.Ternary(payload.HasContentType(), 2, 1)

	var allMods []RequestModifier
	if totalLen <= stackModCap && stackBuf != nil {
		allMods = stackBuf[:0]
	} else {
		allMods = make([]RequestModifier, 0, totalLen)
	}

	if len(payload.Bytes) > 0 {
		allMods = append(allMods, mod.WithBodyBytes(payload.Bytes))
	} else {
		allMods = append(allMods, mod.WithBody(payload.Reader))
	}

	if payload.HasContentType() {
		allMods = append(allMods, mod.WithContentType(payload.ContentType))
	}

	allMods = append(allMods, mods...)

	return allMods, nil
}

func withCaptureMod(
	stackBuf *[stackModCap]RequestModifier,
	target **http.Response,
	mods []RequestModifier,
) []RequestModifier {
	totalLen := len(mods) + 1

	if totalLen <= stackModCap && stackBuf != nil {
		stackBuf[0] = mod.WithCaptureResponse(target)
		copy(stackBuf[1:], mods)

		return stackBuf[:totalLen]
	}

	allMods := make([]RequestModifier, 0, totalLen)
	allMods = append(allMods, mod.WithCaptureResponse(target))
	allMods = append(allMods, mods...)

	return allMods
}
