// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fluent provides a high-performance, chainable Request Builder API backed by zero-allocation request pooling.
package fluent

import (
	"context"
	"net/http"

	"github.com/lemon4ksan/foundation/generic"
	"google.golang.org/protobuf/proto"

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

// DeleteTo dispatches a DELETE request and unmarshals the 2xx response into T.
func DeleteTo[T any](
	ctx context.Context,
	c any,
	path string,
	mods ...aoni.RequestModifier,
) (T, *http.Response, error) {
	return FetchTo[T](ctx, c, http.MethodDelete, path, mods...)
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

// Fetch executes a GET request using the shared default client and returns a Swift-inspired [generic.Result].
func Fetch[T any](
	ctx context.Context,
	path string,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	return FetchResult[T](ctx, nil, http.MethodGet, path, mods...)
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

// GetResult dispatches a GET request and returns a Swift-inspired [generic.Result] wrapping the unmarshaled response.
func GetResult[T any](
	ctx context.Context,
	c any,
	path string,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	return FetchResult[T](ctx, c, http.MethodGet, path, mods...)
}

// PostResult dispatches a POST request with body and returns a Swift-inspired [generic.Result].
func PostResult[T any](
	ctx context.Context,
	c any,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetBody(body).
		SetResult(&target).
		Apply(mods...).
		Post(path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(target), resp
}

// PutResult dispatches a PUT request with body and returns a Swift-inspired [generic.Result].
func PutResult[T any](
	ctx context.Context,
	c any,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetBody(body).
		SetResult(&target).
		Apply(mods...).
		Put(path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(target), resp
}

// DeleteResult dispatches a DELETE request and returns a Swift-inspired [generic.Result].
func DeleteResult[T any](
	ctx context.Context,
	c any,
	path string,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	return FetchResult[T](ctx, c, http.MethodDelete, path, mods...)
}

// PatchResult dispatches a PATCH request with body and returns a Swift-inspired [generic.Result].
func PatchResult[T any](
	ctx context.Context,
	c any,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetBody(body).
		SetResult(&target).
		Apply(mods...).
		Patch(path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(target), resp
}

// PostProtoResult dispatches a Protobuf POST request and returns a [generic.Result].
func PostProtoResult[T any](
	ctx context.Context,
	c any,
	path string,
	reqMsg proto.Message,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetProtoBody(reqMsg).
		SetProtoResult(&target).
		Apply(mods...).
		Post(path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(target), resp
}

// GetProtoResult dispatches a Protobuf GET request and returns a [generic.Result].
func GetProtoResult[T any](
	ctx context.Context,
	c any,
	path string,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetProtoResult(&target).
		Apply(mods...).
		Get(path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(target), resp
}

// PostGRPCWebResult dispatches a gRPC-Web POST request and returns a [generic.Result].
func PostGRPCWebResult[T any](
	ctx context.Context,
	c any,
	path string,
	reqMsg proto.Message,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetGRPCWebBody(reqMsg).
		SetGRPCWebResult(&target).
		Apply(mods...).
		Post(path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(target), resp
}

// GetGRPCWebResult dispatches a gRPC-Web GET request and returns a [generic.Result].
func GetGRPCWebResult[T any](
	ctx context.Context,
	c any,
	path string,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetGRPCWebResult(&target).
		Apply(mods...).
		Get(path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(target), resp
}

// PostXMLResult dispatches an XML POST request and returns a [generic.Result].
func PostXMLResult[T any](
	ctx context.Context,
	c any,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetXMLBody(body).
		SetXMLResult(&target).
		Apply(mods...).
		Post(path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(target), resp
}

// GetXMLResult dispatches an XML GET request and returns a [generic.Result].
func GetXMLResult[T any](
	ctx context.Context,
	c any,
	path string,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetXMLResult(&target).
		Apply(mods...).
		Get(path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(target), resp
}

// PostYAMLResult dispatches a YAML POST request and returns a [generic.Result].
func PostYAMLResult[T any](
	ctx context.Context,
	c any,
	path string,
	body any,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetYAMLBody(body).
		SetYAMLResult(&target).
		Apply(mods...).
		Post(path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(target), resp
}

// GetYAMLResult dispatches a YAML GET request and returns a [generic.Result].
func GetYAMLResult[T any](
	ctx context.Context,
	c any,
	path string,
	mods ...aoni.RequestModifier,
) (generic.Result[T], *http.Response) {
	var target T

	resp, err := R(c).
		SetContext(ctx).
		SetYAMLResult(&target).
		Apply(mods...).
		Get(path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(target), resp
}
