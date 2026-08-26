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

// Request is an alias for [aoni.RequestBuilder].
type Request = aoni.RequestBuilder

// New acquires a pooled [Request] builder bound to the provided client engine or [aoni.Client].
// If no doer argument is provided, the shared default client is used.
func New(doer ...any) *Request {
	if len(doer) == 0 {
		return aoni.R()
	}

	if c, ok := doer[0].(*aoni.Client); ok {
		return c.R()
	}

	if d, ok := doer[0].(aoni.HTTPRequester); ok {
		return aoni.NewClient(d).R()
	}

	return aoni.R()
}

// R is a convenient shorthand alias for [New].
func R(doer ...any) *Request {
	return New(doer...)
}

// FetchTo executes a request with method, path, and optional modifiers, unmarshaling the 2xx response into T.
func FetchTo[T any](
	ctx context.Context,
	c any,
	method, path string,
	mods ...aoni.RequestModifier,
) (T, *http.Response, error) {
	return aoni.FetchTo[T](ctx, c, method, path, mods...)
}

// GetTo dispatches a GET request and unmarshals the 2xx response payload directly into T.
func GetTo[T any](
	ctx context.Context,
	c any,
	path string,
	mods ...aoni.RequestModifier,
) (T, *http.Response, error) {
	var target T
	var doer aoni.HTTPRequester
	if d, ok := c.(aoni.HTTPRequester); ok {
		doer = d
	} else if c == nil {
		doer = aoni.DefaultClient
	}

	resp, err := R(doer).
		SetContext(ctx).
		SetResult(&target).
		Apply(mods...).
		Get(path)

	return target, resp, err
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
	var doer aoni.HTTPRequester
	if d, ok := c.(aoni.HTTPRequester); ok {
		doer = d
	} else if c == nil {
		doer = aoni.DefaultClient
	}

	resp, err := R(doer).
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
	var doer aoni.HTTPRequester
	if d, ok := c.(aoni.HTTPRequester); ok {
		doer = d
	} else if c == nil {
		doer = aoni.DefaultClient
	}

	resp, err := R(doer).
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
	var doer aoni.HTTPRequester
	if d, ok := c.(aoni.HTTPRequester); ok {
		doer = d
	} else if c == nil {
		doer = aoni.DefaultClient
	}

	resp, err := R(doer).
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

// BatchFetchTo dispatches multiple requests concurrently and unmarshals each 2xx response payload into a slice of T.
func BatchFetchTo[T any](
	ctx context.Context,
	c any,
	method string,
	paths []string,
	mods ...aoni.RequestModifier,
) ([]T, error) {
	return aoni.BatchFetchTo[T](ctx, c, method, paths, mods...)
}

// BatchGetTo dispatches multiple GET requests concurrently and unmarshals each 2xx response payload into a slice of T.
func BatchGetTo[T any](
	ctx context.Context,
	c any,
	paths []string,
	mods ...aoni.RequestModifier,
) ([]T, error) {
	return aoni.BatchGetTo[T](ctx, c, paths, mods...)
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
	return aoni.FetchScoped[T](ctx, c, method, path, fn, mods...)
}

// GetScoped dispatches a GET request and passes the decoded response T to fn within an active [borrow.Scope].
func GetScoped[T any](
	ctx context.Context,
	c any,
	path string,
	fn func(scope *borrow.Scope, val T, resp *http.Response) error,
	mods ...aoni.RequestModifier,
) error {
	return aoni.GetScoped[T](ctx, c, path, fn, mods...)
}

// ExecuteTo executes the borrowed Request with method and path, unmarshaling the response into T.
func ExecuteTo[T any](r *Request, method, path string) (T, *http.Response, error) {
	if r == nil {
		var zero T
		return zero, nil, aoni.ErrNilRequest
	}

	return r.ExecuteTo[T](method, path)
}

// ExecuteResult executes the borrowed Request and returns a Swift-inspired [generic.Result].
func ExecuteResult[T any](r *Request, method, path string) (generic.Result[T], *http.Response) {
	if r == nil {
		return generic.Failure[T](aoni.ErrNilRequest), nil
	}

	return r.ExecuteResult[T](method, path)
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
