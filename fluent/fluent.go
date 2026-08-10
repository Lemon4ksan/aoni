// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fluent provides a high-performance, chainable Request Builder API backed by zero-allocation request pooling.
package fluent

import (
	"context"
	"net/http"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
)

// New acquires a pooled [Request] builder bound to the provided client engine or [aoni.Client].
//
// Postconditions:
//   - The returned request must be executed or released via [Request.Discard] to prevent pool leaks.
func New(doer any) *Request {
	return requestPool.Get(doer)
}

// R is a convenient shorthand alias for [New].
func R(doer any) *Request {
	return requestPool.Get(doer)
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
