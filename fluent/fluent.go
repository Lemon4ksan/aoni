// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fluent provides a high-performance, chainable Request Builder API backed by zero-allocation request pooling.
package fluent

import (
	"context"
	"net/http"

	"github.com/lemon4ksan/foundation/borrow"
	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni"
)

// New acquires a pooled [Request] builder bound to the provided client engine or [aoni.Client].
// If no doer argument is provided, the shared default client is used.
//
// The returned [Request] is borrowed from a global object pool and is not thread-safe.
// Callers must finalize the request by invoking one of its execution methods (such as [Request.Execute],
// [Request.Get], [Request.Post]), or explicitly release it back to the pool using [Request.Release].
func New(doer ...any) *Request {
	if len(doer) == 0 {
		return acquireRequest(nil)
	}

	return acquireRequest(doer[0])
}

// R is a convenient shorthand alias for [New].
// If no doer argument is provided, the shared default client is used.
//
// Callers must finalize the borrowed request with an execution method or [Request.Release].
func R(doer ...any) *Request {
	if len(doer) == 0 {
		return acquireRequest(nil)
	}

	return acquireRequest(doer[0])
}

// FetchTo executes a request with method, path, and optional [aoni.RequestModifier] options, unmarshaling the 2xx response into T.
func FetchTo[T any](
	ctx context.Context,
	c any,
	method, path string,
	mods ...aoni.RequestModifier,
) (T, *http.Response, error) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetResult(&target).
		Apply(mods...).
		Execute(method, path)

	return target, resp, err
}

// GetTo dispatches a GET request and unmarshals the 2xx response payload directly into T.
func GetTo[T any](
	ctx context.Context,
	c any,
	path string,
	mods ...aoni.RequestModifier,
) (T, *http.Response, error) {
	return FetchTo[T](ctx, c, http.MethodGet, path, mods...)
}

// FetchScoped executes a request with method, path, and optional modifiers, passing the decoded response
// into fn within an active [borrow.Scope].
func FetchScoped[T any](
	ctx context.Context,
	c any,
	method, path string,
	fn func(scope *borrow.Scope, val T, resp *http.Response) error,
	mods ...aoni.RequestModifier,
) error {
	var target T

	resp, err := R(c).
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

// GetScoped dispatches a GET request and passes the decoded response T to fn within an active [borrow.Scope].
func GetScoped[T any](
	ctx context.Context,
	c any,
	path string,
	fn func(scope *borrow.Scope, val T, resp *http.Response) error,
	mods ...aoni.RequestModifier,
) error {
	return FetchScoped[T](ctx, c, http.MethodGet, path, fn, mods...)
}

// PostTo dispatches a POST request with payload body and unmarshals the 2xx response into T.
func PostTo[T any](
	ctx context.Context,
	c any,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (T, *http.Response, error) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetBody(body).
		SetResult(&target).
		Apply(mods...).
		Post(path)

	return target, resp, err
}

// PutTo dispatches a PUT request with payload body and unmarshals the 2xx response into T.
func PutTo[T any](
	ctx context.Context,
	c any,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (T, *http.Response, error) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetBody(body).
		SetResult(&target).
		Apply(mods...).
		Put(path)

	return target, resp, err
}

// PatchTo dispatches a PATCH request with payload body and unmarshals the 2xx response into T.
func PatchTo[T any](
	ctx context.Context,
	c any,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (T, *http.Response, error) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetBody(body).
		SetResult(&target).
		Apply(mods...).
		Patch(path)

	return target, resp, err
}

// DeleteTo dispatches a DELETE request and unmarshals the 2xx response into T.
func DeleteTo[T any](
	ctx context.Context,
	c any,
	path string,
	mods ...aoni.RequestModifier,
) (T, *http.Response, error) {
	return FetchTo[T](ctx, c, http.MethodDelete, path, mods...)
}

// ExecuteTo executes the borrowed Request with method and path, unmarshaling the response into T.
func ExecuteTo[T any](r *Request, method, path string) (T, *http.Response, error) {
	var target T
	if r == nil {
		return target, nil, ErrNilRequest
	}

	resp, err := r.SetResult(&target).Execute(method, path)

	return target, resp, err
}

// ExecuteResult executes the borrowed Request and returns a Swift-inspired [generic.Result].
func ExecuteResult[T any](r *Request, method, path string) (generic.Result[T], *http.Response) {
	val, resp, err := ExecuteTo[T](r, method, path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(val), resp
}

// FetchResult executes a request and returns a Swift-inspired [generic.Result] wrapping the unmarshaled response or error.
func FetchResult[T any](
	ctx context.Context,
	c any,
	method, path string,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	val, resp, err := FetchTo[T](ctx, c, method, path, mods...)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(val), resp
}
