// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fluent provides a high-performance, chainable Request Builder API
// designed for ergonomics, zero-allocation request pooling, and seamless integration
// with the core aoni HTTP client.
package fluent

import (
	"context"
	"net/http"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
)

// New initializes a new pooled Request builder bound to the target client.
//
// Request builders are pooled to avoid unnecessary allocations and improve performance.
// Call [Request.Discard] if a constructed request is abandoned before execution.
func New(client *aoni.Client) *Request {
	return requestPool.Get(client)
}

// R is a convenient short alias for New.
func R(client *aoni.Client) *Request {
	return requestPool.Get(client)
}

// FetchTo is the universal generic entrypoint that executes a request using any method, path, and codecs/modifiers,
// unmarshaling the 2xx response directly into T.
//
// Replaces specialized functions like GetJSON or PostProto with a single type-safe interface.
func FetchTo[T any](
	ctx context.Context,
	c *aoni.Client,
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

// PostProtoTo dispatches a POST request with a Protobuf payload and unmarshals a binary Protobuf response into T.
func PostProtoTo[T any](
	ctx context.Context,
	c *aoni.Client,
	path string,
	reqMsg proto.Message,
) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetProtoBody(reqMsg).SetProtoResult(&target).Post(path)

	return target, resp, err
}

// PostGRPCWebTo dispatches a POST request with a gRPC-Web framed payload and unmarshals a gRPC-Web response frame into T.
func PostGRPCWebTo[T any](
	ctx context.Context,
	c *aoni.Client,
	path string,
	reqMsg proto.Message,
) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetGRPCWebBody(reqMsg).SetGRPCWebResult(&target).Post(path)

	return target, resp, err
}

// GetProtoTo dispatches a GET request and unmarshals a binary Protobuf response into T.
func GetProtoTo[T any](ctx context.Context, c *aoni.Client, path string) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetProtoResult(&target).Get(path)

	return target, resp, err
}

// GetGRPCWebTo dispatches a GET request and unmarshals a gRPC-Web response frame into T.
func GetGRPCWebTo[T any](ctx context.Context, c *aoni.Client, path string) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetGRPCWebResult(&target).Get(path)

	return target, resp, err
}
