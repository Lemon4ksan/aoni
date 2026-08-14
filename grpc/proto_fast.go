// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"context"
	"net/http"

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/request"
)

// ProtoGetToFast executes a fast GET request expecting binary Protobuf response decoded into Resp.
func ProtoGetToFast[Resp any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withProtoGetMods(&stackBuf, mods)

	return request.DoToFast[Resp](ctx, doer, http.MethodGet, path, nil, allMods...)
}

// ProtoGetIntoFast executes a fast GET request expecting binary Protobuf response decoded into target.
func ProtoGetIntoFast[T any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	target *T,
	mods ...aoni.RequestModifier,
) error {
	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withProtoGetMods(&stackBuf, mods)

	return request.DoIntoFast[T](ctx, doer, http.MethodGet, path, nil, target, allMods...)
}

// ProtoPostToFast executes a fast POST request carrying a binary proto.Message payload decoded into Resp.
func ProtoPostToFast[Resp any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withProtoPostMods(&stackBuf, msg, mods)

	return request.DoToFast[Resp](ctx, doer, http.MethodPost, path, nil, allMods...)
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
	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withProtoPostMods(&stackBuf, msg, mods)

	return request.DoIntoFast[T](ctx, doer, http.MethodPost, path, nil, target, allMods...)
}

// WebPostToFast executes a fast POST request carrying a gRPC-Web framed payload decoded into Resp.
func WebPostToFast[Resp any](
	ctx context.Context,
	doer aoni.RequestDoer,
	path string,
	msg proto.Message,
	mods ...aoni.RequestModifier,
) (*Resp, error) {
	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withWebPostMods(&stackBuf, msg, mods)

	return request.DoToFast[Resp](ctx, doer, http.MethodPost, path, nil, allMods...)
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
	var stackBuf [stackModCapacity]aoni.RequestModifier

	allMods := withWebPostMods(&stackBuf, msg, mods)

	return request.DoIntoFast[T](ctx, doer, http.MethodPost, path, nil, target, allMods...)
}
