// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/grpc"
)

// GRPCClient provides native, zero-dependency gRPC operations over aoni's stealth HTTP/2 transport.
// It inherits uTLS browser fingerprinting, p0f OS spoofing, and HPACK header order preservation.
type GRPCClient struct {
	client *Client
}

// GRPC returns the [GRPCClient] sub-client bound to this client instance.
func (c *Client) GRPC() *GRPCClient {
	return &GRPCClient{client: c}
}

// Invoke executes a native unary gRPC call over stealth HTTP/2 transport and unmarshals the response frame into *Resp.
//
// Preconditions:
//   - ctx and reqMsg must be non-nil.
//   - Resp must implement [proto.Message].
func (g *GRPCClient) Invoke[Resp any](
	ctx context.Context,
	fullMethod string,
	reqMsg proto.Message,
	mods ...RequestModifier,
) (*Resp, error) {
	return grpc.Invoke[Resp](ctx, g.client, fullMethod, reqMsg, mods...)
}

// InvokeInto executes a native unary gRPC call and unmarshals the response frame directly into target message without allocation.
func (g *GRPCClient) InvokeInto(
	ctx context.Context,
	fullMethod string,
	reqMsg proto.Message,
	target proto.Message,
	mods ...RequestModifier,
) error {
	return grpc.InvokeInto(ctx, g.client, fullMethod, reqMsg, target, mods...)
}

// InvokeGRPC executes a native unary gRPC call over stealth HTTP/2 transport using the specified requester (or DefaultClient if nil).
func InvokeGRPC[Resp any](
	ctx context.Context,
	doer HTTPRequester,
	fullMethod string,
	reqMsg proto.Message,
	mods ...RequestModifier,
) (*Resp, error) {
	if doer == nil {
		doer = DefaultClient
	}

	return grpc.Invoke[Resp](ctx, doer, fullMethod, reqMsg, mods...)
}

// InvokeGRPCInto executes a native unary gRPC call and unmarshals the response frame directly into target message without allocation.
func InvokeGRPCInto(
	ctx context.Context,
	doer HTTPRequester,
	fullMethod string,
	reqMsg proto.Message,
	target proto.Message,
	mods ...RequestModifier,
) error {
	if doer == nil {
		doer = DefaultClient
	}

	return grpc.InvokeInto(ctx, doer, fullMethod, reqMsg, target, mods...)
}
