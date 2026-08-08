// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/request"
)

// Re-exported Protobuf and gRPC-Web decoders for single-package discoverability.
var (
	// ProtoDecoder reads binary Protocol Buffer payloads into proto.Message targets.
	ProtoDecoder = decode.ProtoDecoder

	// GRPCWebDecoder extracts Protobuf payloads from 5-byte gRPC-Web frames and validates trailers.
	GRPCWebDecoder = decode.GRPCWebDecoder

	// ProtoJSONDecoder parses JSON response streams into Protobuf messages via protojson.
	ProtoJSONDecoder = decode.ProtoJSONDecoder

	// WithProto creates an [aoni.RequestModifier] that assigns ProtoDecoder for response parsing.
	WithProto = decode.WithProto

	// WithGRPCWeb creates an [aoni.RequestModifier] that assigns GRPCWebDecoder for response parsing.
	WithGRPCWeb = decode.WithGRPCWeb
)

// ProtoGetTo executes an HTTP GET request expecting a binary Protocol Buffer response stream unmarshaled into Resp.
func ProtoGetTo[Resp any](
	ctx context.Context,
	c request.Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	mods = append([]aoni.RequestModifier{
		mod.WithHeader("Accept", "application/x-protobuf"),
		mod.WithDecoder(decode.ProtoDecoder),
	}, mods...)

	return request.GetTo[Resp](ctx, c, path, mods...)
}

// ProtoGetInto executes an HTTP GET request expecting a binary Protocol Buffer stream unmarshaled directly into target.
func ProtoGetInto[T any](
	ctx context.Context,
	c request.Requester,
	path string,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	mods = append([]aoni.RequestModifier{
		mod.WithHeader("Accept", "application/x-protobuf"),
		mod.WithDecoder(decode.ProtoDecoder),
	}, mods...)

	return request.GetInto(ctx, c, path, target, mods...)
}

// ProtoPostTo executes an HTTP POST request carrying a binary [proto.Message] payload and unmarshals the response into Resp.
func ProtoPostTo[Resp any](
	ctx context.Context,
	c request.Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	mods = append([]aoni.RequestModifier{
		mod.WithProtoBody(msg),
		mod.WithDecoder(decode.ProtoDecoder),
	}, mods...)

	return request.PostTo[Resp](ctx, c, path, nil, mods...)
}

// ProtoPostInto executes an HTTP POST request carrying a binary [proto.Message] payload and unmarshals the response directly into target.
func ProtoPostInto[T any](
	ctx context.Context,
	c request.Requester,
	path string,
	msg proto.Message,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	mods = append([]aoni.RequestModifier{
		mod.WithProtoBody(msg),
		mod.WithDecoder(decode.ProtoDecoder),
	}, mods...)

	return request.PostInto(ctx, c, path, nil, target, mods...)
}

// WebPostTo executes an HTTP POST request carrying a 5-byte gRPC-Web framed payload and unmarshals the response into Resp.
func WebPostTo[Resp any](
	ctx context.Context,
	c request.Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	mods = append([]aoni.RequestModifier{
		mod.WithGRPCWebBody(msg),
		mod.WithDecoder(decode.GRPCWebDecoder),
	}, mods...)

	return request.PostTo[Resp](ctx, c, path, nil, mods...)
}

// WebPostInto executes an HTTP POST request carrying a 5-byte gRPC-Web framed payload and unmarshals the response directly into target.
func WebPostInto[T any](
	ctx context.Context,
	c request.Requester,
	path string,
	msg proto.Message,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	mods = append([]aoni.RequestModifier{
		mod.WithGRPCWebBody(msg),
		mod.WithDecoder(decode.GRPCWebDecoder),
	}, mods...)

	return request.PostInto(ctx, c, path, nil, target, mods...)
}
