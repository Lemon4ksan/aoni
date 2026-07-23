// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package codec provides request encoding and response decoding strategies.
package codec

import (
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/mod"
)

// Codec defines a reusable strategy for marshaling request bodies and unmarshaling responses.
type Codec interface {
	Encode(body any) aoni.RequestModifier
	Decode() aoni.RequestModifier
}

type jsonCodec struct{}

func (jsonCodec) Encode(body any) aoni.RequestModifier { return mod.WithJSONBody(body) }
func (jsonCodec) Decode() aoni.RequestModifier         { return decode.WithJSON() }

// JSONCodec provides standard JSON request encoding and response decoding strategies.
var JSONCodec Codec = jsonCodec{}

type protoCodec struct{}

func (protoCodec) Encode(body any) aoni.RequestModifier {
	if msg, ok := body.(proto.Message); ok {
		return mod.WithProtoBody(msg)
	}

	return nil
}
func (protoCodec) Decode() aoni.RequestModifier { return decode.WithProto() }

// ProtoCodec provides binary Protocol Buffer encoding and decoding strategies.
var ProtoCodec Codec = protoCodec{}

type grpcWebCodec struct{}

func (grpcWebCodec) Encode(body any) aoni.RequestModifier {
	if msg, ok := body.(proto.Message); ok {
		return mod.WithGRPCWebBody(msg)
	}

	return nil
}
func (grpcWebCodec) Decode() aoni.RequestModifier { return decode.WithGRPCWeb() }

// GRPCWebCodec provides gRPC-Web framed Protocol Buffer encoding and decoding strategies.
var GRPCWebCodec Codec = grpcWebCodec{}
