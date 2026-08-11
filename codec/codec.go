// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package codec defines high-level request body encoding and response body decoding contracts.
package codec

import (
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/aoni/mod"
)

// Codec defines a unified strategy for marshaling outbound request payloads and unmarshaling incoming response streams.
//
// Black-Box Contract:
// Implementations construct [aoni.RequestModifier] instances that bind specific content-type encoders
// (JSON, Protobuf, gRPC-Web) and assign matched stream decoders.
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
//
// Postconditions:
//   - Outbound requests set 'Content-Type: application/json' and 'Accept: application/json'.
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
//
// Postconditions:
//   - Outbound requests set 'Content-Type: application/x-protobuf' and 'Accept: application/x-protobuf'.
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
//
// Specification Adherence:
// Conforms to gRPC-Web specification: encodes messages with 5-byte frame headers (1-byte compressed flag + 4-byte big-endian message length)
// and validates trailers-only / trailer stream frames.
//
// Postconditions:
//   - Outbound requests set 'Content-Type: application/grpc-web+proto' and 'X-Grpc-Web: 1'.
var GRPCWebCodec Codec = grpcWebCodec{}

// Decoder re-export from decode package.
type Decoder = decode.Decoder

// StructToValues encodes a struct into [url.Values].
var StructToValues = values.StructToValues

// StructToQueryString converts a struct into a URL query parameter string.
var StructToQueryString = values.StructToQueryString
