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
//
// The returned [Request] is borrowed from a global object pool and is not thread-safe.
// Callers must finalize the request by invoking one of its execution methods (such as [Request.Execute],
// [Request.Get], [Request.Post]), or explicitly release it back to the pool using [Request.Discard].
func New(doer any) *Request {
	return acquireRequest(doer)
}

// R is a convenient shorthand alias for [New].
//
// Callers must finalize the borrowed request with an execution method or [Request.Discard].
func R(doer any) *Request {
	return acquireRequest(doer)
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

// PostProtoTo dispatches a POST request carrying a binary [proto.Message] payload and unmarshals the response into T.
func PostProtoTo[T any](
	ctx context.Context,
	c any,
	path string,
	reqMsg proto.Message,
) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetProtoBody(reqMsg).SetProtoResult(&target).Post(path)

	return target, resp, err
}

// PostGRPCWebTo dispatches a POST request carrying a gRPC-Web framed payload and unmarshals the response frame into T.
func PostGRPCWebTo[T any](
	ctx context.Context,
	c any,
	path string,
	reqMsg proto.Message,
) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetGRPCWebBody(reqMsg).SetGRPCWebResult(&target).Post(path)

	return target, resp, err
}

// GetProtoTo dispatches a GET request expecting a binary Protocol Buffer response stream decoded into T.
func GetProtoTo[T any](ctx context.Context, c any, path string) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetProtoResult(&target).Get(path)

	return target, resp, err
}

// GetGRPCWebTo dispatches a GET request expecting a gRPC-Web framed response stream decoded into T.
func GetGRPCWebTo[T any](ctx context.Context, c any, path string) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetGRPCWebResult(&target).Get(path)

	return target, resp, err
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

// PostProtoResult dispatches a Protobuf POST request and returns a [generic.Result].
func PostProtoResult[T any](
	ctx context.Context,
	c any,
	path string,
	reqMsg proto.Message,
) (generic.Result[T], *http.Response) {
	val, resp, err := PostProtoTo[T](ctx, c, path, reqMsg)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(val), resp
}

// GetProtoResult dispatches a Protobuf GET request and returns a [generic.Result].
func GetProtoResult[T any](ctx context.Context, c any, path string) (generic.Result[T], *http.Response) {
	val, resp, err := GetProtoTo[T](ctx, c, path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(val), resp
}

// PostGRPCWebResult dispatches a gRPC-Web POST request and returns a [generic.Result].
func PostGRPCWebResult[T any](
	ctx context.Context,
	c any,
	path string,
	reqMsg proto.Message,
) (generic.Result[T], *http.Response) {
	val, resp, err := PostGRPCWebTo[T](ctx, c, path, reqMsg)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(val), resp
}

// GetGRPCWebResult dispatches a gRPC-Web GET request and returns a [generic.Result].
func GetGRPCWebResult[T any](ctx context.Context, c any, path string) (generic.Result[T], *http.Response) {
	val, resp, err := GetGRPCWebTo[T](ctx, c, path)
	if err != nil {
		return generic.Failure[T](err), resp
	}

	return generic.Success(val), resp
}
