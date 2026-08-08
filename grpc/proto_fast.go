// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"context"
	"net/http"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/request"
)

// ProtoGetToFast executes a fast GET request expecting binary Protobuf response decoded into Resp.
func ProtoGetToFast[Resp any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	mods = append([]aoni.RequestModifier{
		mod.WithHeader("Accept", "application/x-protobuf"),
		mod.WithDecoder(decode.ProtoDecoder),
	}, mods...)

	return request.DoToFast[Resp](ctx, doer, http.MethodGet, path, nil, mods...)
}

// ProtoGetIntoFast executes a fast GET request expecting binary Protobuf response decoded into target.
func ProtoGetIntoFast[T any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	mods = append([]aoni.RequestModifier{
		mod.WithHeader("Accept", "application/x-protobuf"),
		mod.WithDecoder(decode.ProtoDecoder),
	}, mods...)

	return request.DoIntoFast[T](ctx, doer, http.MethodGet, path, nil, target, mods...)
}

// ProtoPostToFast executes a fast POST request carrying a binary proto.Message payload decoded into Resp.
func ProtoPostToFast[Resp any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	mods = append([]aoni.RequestModifier{
		mod.WithProtoBody(msg),
		mod.WithDecoder(decode.ProtoDecoder),
	}, mods...)

	return request.DoToFast[Resp](ctx, doer, http.MethodPost, path, nil, mods...)
}

// ProtoPostIntoFast executes a fast POST request carrying a binary proto.Message payload decoded into target.
func ProtoPostIntoFast[T any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	msg proto.Message,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	mods = append([]aoni.RequestModifier{
		mod.WithProtoBody(msg),
		mod.WithDecoder(decode.ProtoDecoder),
	}, mods...)

	return request.DoIntoFast[T](ctx, doer, http.MethodPost, path, nil, target, mods...)
}

// WebPostToFast executes a fast POST request carrying a gRPC-Web framed payload decoded into Resp.
func WebPostToFast[Resp any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	mods = append([]aoni.RequestModifier{
		mod.WithGRPCWebBody(msg),
		mod.WithDecoder(decode.GRPCWebDecoder),
	}, mods...)

	return request.DoToFast[Resp](ctx, doer, http.MethodPost, path, nil, mods...)
}

// WebPostIntoFast executes a fast POST request carrying a gRPC-Web framed payload decoded into target.
func WebPostIntoFast[T any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	msg proto.Message,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	mods = append([]aoni.RequestModifier{
		mod.WithGRPCWebBody(msg),
		mod.WithDecoder(decode.GRPCWebDecoder),
	}, mods...)

	return request.DoIntoFast[T](ctx, doer, http.MethodPost, path, nil, target, mods...)
}
