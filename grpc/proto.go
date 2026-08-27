// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/lemon4ksan/foundation/net/http/header"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/requestutil"
	"github.com/lemon4ksan/aoni/mod"
)

// Re-exported Protobuf and gRPC-Web decoders for single-package discoverability.
var (
	// ProtoDecoder reads binary Protocol Buffer payloads into proto.Message targets.
	ProtoDecoder = decode.ProtoDecoder

	// GRPCWebDecoder extracts Protobuf payloads from 5-byte gRPC-Web frames and validates trailers.
	GRPCWebDecoder = decode.GRPCWebDecoder

	// ProtoJSONDecoder parses JSON response streams into Protobuf messages via protojson.
	ProtoJSONDecoder = decode.ProtoJSONDecoder

	// WithProto creates a [core.RequestModifier] that assigns ProtoDecoder for response parsing.
	WithProto = decode.WithProto

	// WithGRPCWeb creates a [core.RequestModifier] that assigns GRPCWebDecoder for response parsing.
	WithGRPCWeb = decode.WithGRPCWeb
)

// WithGRPCWebTimeout assigns standard gRPC-Web timeout headers.
func WithGRPCWebTimeout(d time.Duration) core.RequestModifier {
	return mod.WithHeader(header.GRPCTimeout, requestutil.FormatGRPCTimeout(d))
}

// ProtoGetTo executes an HTTP GET request expecting a binary Protocol Buffer response stream unmarshaled into Resp.
func ProtoGetTo[Resp any](
	ctx context.Context,
	doer core.HTTPRequester,
	path string,
	mods ...core.RequestModifier,
) (*Resp, error) {
	var stackBuf [stackModCapacity]core.RequestModifier

	allMods := withProtoGetMods(&stackBuf, mods)

	resp, err := doer.Request(ctx, http.MethodGet, path, allMods...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	result := new(Resp)
	if err := decode.ProtoDecoder.Decode(resp.Body, result); err != nil {
		return nil, err
	}

	return result, nil
}

// ProtoGetInto executes an HTTP GET request expecting a binary Protocol Buffer stream unmarshaled directly into target.
func ProtoGetInto[T any](
	ctx context.Context,
	doer core.HTTPRequester,
	path string,
	target *T,
	mods ...core.RequestModifier,
) error {
	var stackBuf [stackModCapacity]core.RequestModifier

	allMods := withProtoGetMods(&stackBuf, mods)

	resp, err := doer.Request(ctx, http.MethodGet, path, allMods...)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return decode.ProtoDecoder.Decode(resp.Body, target)
}

// ProtoPostTo executes an HTTP POST request carrying a binary [proto.Message] payload and unmarshals the response into Resp.
func ProtoPostTo[Resp any](
	ctx context.Context,
	doer core.HTTPRequester,
	path string,
	msg proto.Message,
	mods ...core.RequestModifier,
) (*Resp, error) {
	var stackBuf [stackModCapacity]core.RequestModifier

	allMods := withProtoPostMods(&stackBuf, msg, mods)

	resp, err := doer.Request(ctx, http.MethodPost, path, allMods...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	result := new(Resp)
	if err := decode.ProtoDecoder.Decode(resp.Body, result); err != nil {
		return nil, err
	}

	return result, nil
}

// ProtoPostInto executes an HTTP POST request carrying a binary [proto.Message] payload and unmarshals the response directly into target.
func ProtoPostInto[T any](
	ctx context.Context,
	doer core.HTTPRequester,
	path string,
	msg proto.Message,
	target *T,
	mods ...core.RequestModifier,
) error {
	var stackBuf [stackModCapacity]core.RequestModifier

	allMods := withProtoPostMods(&stackBuf, msg, mods)

	resp, err := doer.Request(ctx, http.MethodPost, path, allMods...)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return decode.ProtoDecoder.Decode(resp.Body, target)
}

// WebPostTo executes an HTTP POST request carrying a 5-byte gRPC-Web framed payload and unmarshals the response into Resp.
func WebPostTo[Resp any](
	ctx context.Context,
	doer core.HTTPRequester,
	path string,
	msg proto.Message,
	mods ...core.RequestModifier,
) (*Resp, error) {
	var stackBuf [stackModCapacity]core.RequestModifier

	allMods := withWebPostMods(&stackBuf, msg, mods)

	resp, err := doer.Request(ctx, http.MethodPost, path, allMods...)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	result := new(Resp)
	if err := decode.GRPCWebDecoder.Decode(resp.Body, result); err != nil {
		return nil, err
	}

	return result, nil
}

// WebPostInto executes an HTTP POST request carrying a 5-byte gRPC-Web framed payload and unmarshals the response directly into target.
func WebPostInto[T any](
	ctx context.Context,
	doer core.HTTPRequester,
	path string,
	msg proto.Message,
	target *T,
	mods ...core.RequestModifier,
) error {
	var stackBuf [stackModCapacity]core.RequestModifier

	allMods := withWebPostMods(&stackBuf, msg, mods)

	resp, err := doer.Request(ctx, http.MethodPost, path, allMods...)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return decode.GRPCWebDecoder.Decode(resp.Body, target)
}

const stackModCapacity = 16

func withProtoGetMods(
	stackBuf *[stackModCapacity]core.RequestModifier,
	mods []core.RequestModifier,
) []core.RequestModifier {
	total := 2 + len(mods)
	if total <= stackModCapacity {
		stackBuf[0] = mod.WithHeader(header.Accept, header.MIMEApplicationProtobuf)
		stackBuf[1] = mod.WithDecoder(decode.ProtoDecoder)
		copy(stackBuf[2:], mods)

		return stackBuf[:total]
	}

	allMods := make([]core.RequestModifier, 0, total)
	allMods = append(
		allMods,
		mod.WithHeader(header.Accept, header.MIMEApplicationProtobuf),
		mod.WithDecoder(decode.ProtoDecoder),
	)
	allMods = append(allMods, mods...)

	return allMods
}

func withProtoPostMods(
	stackBuf *[stackModCapacity]core.RequestModifier,
	msg proto.Message,
	mods []core.RequestModifier,
) []core.RequestModifier {
	bodyBytes, _ := proto.Marshal(msg)

	total := 3 + len(mods)
	if total <= stackModCapacity {
		stackBuf[0] = mod.WithBody(bytes.NewReader(bodyBytes))
		stackBuf[1] = mod.WithHeader(header.ContentType, header.MIMEApplicationProtobuf)
		stackBuf[2] = mod.WithDecoder(decode.ProtoDecoder)
		copy(stackBuf[3:], mods)

		return stackBuf[:total]
	}

	allMods := make([]core.RequestModifier, 0, total)
	allMods = append(
		allMods,
		mod.WithBody(bytes.NewReader(bodyBytes)),
		mod.WithHeader(header.ContentType, header.MIMEApplicationProtobuf),
		mod.WithDecoder(decode.ProtoDecoder),
	)
	allMods = append(allMods, mods...)

	return allMods
}

func withWebPostMods(
	stackBuf *[stackModCapacity]core.RequestModifier,
	msg proto.Message,
	mods []core.RequestModifier,
) []core.RequestModifier {
	frameBytes, _ := MarshalFrame(msg, false)

	total := 3 + len(mods)
	if total <= stackModCapacity {
		stackBuf[0] = mod.WithBody(bytes.NewReader(frameBytes))
		stackBuf[1] = mod.WithHeader(header.ContentType, header.MIMEApplicationGRPCWebProto)
		stackBuf[2] = mod.WithDecoder(decode.GRPCWebDecoder)
		copy(stackBuf[3:], mods)

		return stackBuf[:total]
	}

	allMods := make([]core.RequestModifier, 0, total)
	allMods = append(
		allMods,
		mod.WithBody(bytes.NewReader(frameBytes)),
		mod.WithHeader(header.ContentType, header.MIMEApplicationGRPCWebProto),
		mod.WithDecoder(decode.GRPCWebDecoder),
	)
	allMods = append(allMods, mods...)

	return allMods
}
