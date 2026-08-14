// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/middleware"
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

	// RetryOnGRPCStatus triggers retries when gRPC trailer status matches codes.
	RetryOnGRPCStatus = middleware.RetryOnGRPCStatus

	// WithGRPCWebTimeout assigns standard gRPC-Web timeout headers.
	WithGRPCWebTimeout = middleware.GRPCWebTimeout

	// WithGRPCMetadata assigns gRPC-Web binary metadata headers.
	WithGRPCMetadata = middleware.GRPCMetadata
)

// ProtoGetTo executes an HTTP GET request expecting a binary Protocol Buffer response stream unmarshaled into Resp.
func ProtoGetTo[Resp any](
	ctx context.Context,
	c request.Requester,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withProtoGetMods(&stackBuf, mods)

	return request.GetTo[Resp](ctx, c, path, allMods...)
}

// ProtoGetInto executes an HTTP GET request expecting a binary Protocol Buffer stream unmarshaled directly into target.
func ProtoGetInto[T any](
	ctx context.Context,
	c request.Requester,
	path string,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withProtoGetMods(&stackBuf, mods)

	return request.GetInto(ctx, c, path, target, allMods...)
}

// ProtoPostTo executes an HTTP POST request carrying a binary [proto.Message] payload and unmarshals the response into Resp.
func ProtoPostTo[Resp any](
	ctx context.Context,
	c request.Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withProtoPostMods(&stackBuf, msg, mods)

	return request.PostTo[Resp](ctx, c, path, nil, allMods...)
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
	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withProtoPostMods(&stackBuf, msg, mods)

	return request.PostInto(ctx, c, path, nil, target, allMods...)
}

// WebPostTo executes an HTTP POST request carrying a 5-byte gRPC-Web framed payload and unmarshals the response into Resp.
func WebPostTo[Resp any](
	ctx context.Context,
	c request.Requester,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withWebPostMods(&stackBuf, msg, mods)

	return request.PostTo[Resp](ctx, c, path, nil, allMods...)
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
	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withWebPostMods(&stackBuf, msg, mods)

	return request.PostInto(ctx, c, path, nil, target, allMods...)
}

const stackModCapacity = 16

func withProtoGetMods(
	stackBuf *[stackModCapacity]aoni.RequestModifier,
	mods []aoni.RequestModifier,
) []aoni.RequestModifier {
	total := 2 + len(mods)
	if total <= stackModCapacity {
		stackBuf[0] = mod.WithHeader("Accept", "application/x-protobuf")
		stackBuf[1] = mod.WithDecoder(decode.ProtoDecoder)
		copy(stackBuf[2:], mods)

		return stackBuf[:total]
	}

	res := make([]aoni.RequestModifier, total)
	res[0] = mod.WithHeader("Accept", "application/x-protobuf")
	res[1] = mod.WithDecoder(decode.ProtoDecoder)
	copy(res[2:], mods)

	return res
}

func withProtoPostMods(
	stackBuf *[stackModCapacity]aoni.RequestModifier,
	msg proto.Message,
	mods []aoni.RequestModifier,
) []aoni.RequestModifier {
	total := 2 + len(mods)
	if total <= stackModCapacity {
		stackBuf[0] = mod.WithProtoBody(msg)
		stackBuf[1] = mod.WithDecoder(decode.ProtoDecoder)
		copy(stackBuf[2:], mods)

		return stackBuf[:total]
	}

	res := make([]aoni.RequestModifier, total)
	res[0] = mod.WithProtoBody(msg)
	res[1] = mod.WithDecoder(decode.ProtoDecoder)
	copy(res[2:], mods)

	return res
}

func withWebPostMods(
	stackBuf *[stackModCapacity]aoni.RequestModifier,
	msg proto.Message,
	mods []aoni.RequestModifier,
) []aoni.RequestModifier {
	total := 2 + len(mods)
	if total <= stackModCapacity {
		stackBuf[0] = mod.WithGRPCWebBody(msg)
		stackBuf[1] = mod.WithDecoder(decode.GRPCWebDecoder)
		copy(stackBuf[2:], mods)

		return stackBuf[:total]
	}

	res := make([]aoni.RequestModifier, total)
	res[0] = mod.WithGRPCWebBody(msg)
	res[1] = mod.WithDecoder(decode.GRPCWebDecoder)
	copy(res[2:], mods)

	return res
}
