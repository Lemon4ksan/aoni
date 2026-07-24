// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package codec defines high-level request body encoding and response body decoding contracts.
package codec

import (
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/mod"
)

// Codec defines a unified strategy for marshaling outbound request payloads and unmarshaling incoming response streams.
type Codec interface {
	// Encode constructs an [aoni.RequestModifier] that serializes body into the outgoing request payload.
	Encode(body any) aoni.RequestModifier

	// Decode constructs an [aoni.RequestModifier] that assigns a matching response body parser.
	Decode() aoni.RequestModifier
}

type jsonCodec struct{}

func (jsonCodec) Encode(body any) aoni.RequestModifier { return mod.WithJSONBody(body) }
func (jsonCodec) Decode() aoni.RequestModifier         { return decode.WithJSON() }

// JSONCodec provides standard JSON request payload encoding and response stream decoding strategies.
var JSONCodec Codec = jsonCodec{}

type protoCodec struct{}

func (protoCodec) Encode(body any) aoni.RequestModifier {
	msg, ok := body.(proto.Message)
	if !ok {
		return nil
	}

	return mod.WithProtoBody(msg)
}

func (protoCodec) Decode() aoni.RequestModifier { return decode.WithProto() }

// ProtoCodec provides binary Protocol Buffer request encoding and response stream decoding strategies.
var ProtoCodec Codec = protoCodec{}

type grpcWebCodec struct{}

func (grpcWebCodec) Encode(body any) aoni.RequestModifier {
	msg, ok := body.(proto.Message)
	if !ok {
		return nil
	}

	return mod.WithGRPCWebBody(msg)
}

func (grpcWebCodec) Decode() aoni.RequestModifier { return decode.WithGRPCWeb() }

// GRPCWebCodec provides 5-byte framed gRPC-Web Protocol Buffer request encoding and decoding strategies.
var GRPCWebCodec Codec = grpcWebCodec{}
