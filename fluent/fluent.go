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
func New(client *aoni.Client) *Request {
	r := requestPool.Get().(*Request)
	r.client = client
	return r
}

// R is a convenient short alias for New.
func R(client *aoni.Client) *Request {
	return New(client)
}

// GetJSON dispatches a GET request and unmarshals a 2xx JSON response directly into T.
func GetJSON[T any](ctx context.Context, c *aoni.Client, path string) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetResult(&target).Get(path)

	return target, resp, err
}

// PostJSON dispatches a POST request with body and unmarshals a 2xx JSON response into T.
func PostJSON[T any](ctx context.Context, c *aoni.Client, path string, body any) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetBody(body).SetResult(&target).Post(path)

	return target, resp, err
}

// PutJSON dispatches a PUT request with body and unmarshals a 2xx JSON response into T.
func PutJSON[T any](ctx context.Context, c *aoni.Client, path string, body any) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetBody(body).SetResult(&target).Put(path)

	return target, resp, err
}

// PatchJSON dispatches a PATCH request with body and unmarshals a 2xx JSON response into T.
func PatchJSON[T any](ctx context.Context, c *aoni.Client, path string, body any) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetBody(body).SetResult(&target).Patch(path)

	return target, resp, err
}

// DeleteJSON dispatches a DELETE request and unmarshals a 2xx JSON response into T.
func DeleteJSON[T any](ctx context.Context, c *aoni.Client, path string) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetResult(&target).Delete(path)

	return target, resp, err
}

// DoJSON dispatches a request with any custom method and optional body, unmarshaling a 2xx JSON response into T.
func DoJSON[T any](ctx context.Context, c *aoni.Client, method, path string, body any) (T, *http.Response, error) {
	var target T

	req := R(c).SetContext(ctx).SetResult(&target)
	if body != nil {
		req.SetBody(body)
	}

	resp, err := req.Do(method, path)

	return target, resp, err
}

// PostProto dispatches a POST request with a Protobuf payload and unmarshals a binary Protobuf response into T.
func PostProto[T any](
	ctx context.Context,
	c *aoni.Client,
	path string,
	reqMsg proto.Message,
) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetProtoBody(reqMsg).SetProtoResult(&target).Post(path)

	return target, resp, err
}

// PostGRPCWeb dispatches a POST request with a gRPC-Web framed payload and unmarshals a gRPC-Web response frame into T.
func PostGRPCWeb[T any](
	ctx context.Context,
	c *aoni.Client,
	path string,
	reqMsg proto.Message,
) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetGRPCWebBody(reqMsg).SetGRPCWebResult(&target).Post(path)

	return target, resp, err
}

// GetProto dispatches a GET request and unmarshals a binary Protobuf response into T.
func GetProto[T any](ctx context.Context, c *aoni.Client, path string) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetProtoResult(&target).Get(path)

	return target, resp, err
}

// GetGRPCWeb dispatches a GET request and unmarshals a gRPC-Web response frame into T.
func GetGRPCWeb[T any](ctx context.Context, c *aoni.Client, path string) (T, *http.Response, error) {
	var target T

	resp, err := R(c).SetContext(ctx).SetGRPCWebResult(&target).Get(path)

	return target, resp, err
}
