// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codec

import (
	"errors"
	"io"

	"github.com/lemon4ksan/foundation/generic"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/aoni/mod"
)

var errNotProtoMessage = errors.New("aoni/codec: body does not implement proto.Message")

// Codec defines a unified strategy for marshaling outbound request payloads and unmarshaling incoming response streams.
//
// Implementations construct [aoni.RequestModifier] instances that bind specific content-type encoders
// (JSON, XML, YAML, Protobuf, gRPC-Web) and assign matched stream decoders.
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
// Outbound requests set 'Content-Type: application/json' and 'Accept: application/json'.
var JSONCodec Codec = jsonCodec{}

type xmlCodec struct{}

func (xmlCodec) Encode(body any) aoni.RequestModifier { return mod.WithXMLBody(body) }
func (xmlCodec) Decode() aoni.RequestModifier         { return decode.WithXML() }

// XMLCodec provides standard XML request payload encoding and response stream decoding strategies.
// Outbound requests set 'Content-Type: application/xml' and 'Accept: application/xml'.
var XMLCodec Codec = xmlCodec{}

type yamlCodec struct{}

func (yamlCodec) Encode(body any) aoni.RequestModifier { return mod.WithYAMLBody(body) }
func (yamlCodec) Decode() aoni.RequestModifier         { return decode.WithYAML() }

// YAMLCodec provides YAML request payload encoding and response stream decoding strategies.
// Outbound requests set 'Content-Type: application/yaml' and 'Accept: application/yaml'.
var YAMLCodec Codec = yamlCodec{}

type protoCodec struct{}

func (protoCodec) Encode(body any) aoni.RequestModifier {
	msg, ok := body.(proto.Message)
	if !ok {
		return mod.Custom(func(req aoni.Request) {
			aoni.MarkModifierError(req, errNotProtoMessage)
		})
	}

	return mod.WithProtoBody(msg)
}

func (protoCodec) Decode() aoni.RequestModifier { return decode.WithProto() }

// ProtoCodec provides binary Protocol Buffer request encoding and response stream decoding strategies.
// Outbound requests set 'Content-Type: application/x-protobuf' and 'Accept: application/x-protobuf'.
var ProtoCodec Codec = protoCodec{}

type grpcWebCodec struct{}

func (grpcWebCodec) Encode(body any) aoni.RequestModifier {
	msg, ok := body.(proto.Message)
	if !ok {
		return mod.Custom(func(req aoni.Request) {
			aoni.MarkModifierError(req, errNotProtoMessage)
		})
	}

	return mod.WithGRPCWebBody(msg)
}

func (grpcWebCodec) Decode() aoni.RequestModifier { return decode.WithGRPCWeb() }

// GRPCWebCodec provides 5-byte framed gRPC-Web Protocol Buffer request encoding and decoding strategies.
// Conforms to gRPC-Web specification: encodes messages with 5-byte frame headers (1-byte compressed flag + 4-byte big-endian message length)
// and validates trailers-only / trailer stream frames.
// Outbound requests set 'Content-Type: application/grpc-web+proto' and 'X-Grpc-Web: 1'.
var GRPCWebCodec Codec = grpcWebCodec{}

// Decoder re-exports [decode.Decoder] for response body decoding.
type Decoder = decode.Decoder

// DecoderFunc adapts a plain function signature to satisfy the [Decoder] interface.
type DecoderFunc = decode.DecoderFunc

var (
	// JSONDecoder parses response payload streams as standard JSON.
	JSONDecoder = decode.JSONDecoder

	// XMLDecoder parses response payload streams as XML.
	XMLDecoder = decode.XMLDecoder

	// YAMLDecoder parses response payload streams as YAML.
	YAMLDecoder = decode.YAMLDecoder

	// ProtoDecoder reads binary Protocol Buffer payloads into [proto.Message] targets.
	ProtoDecoder = decode.ProtoDecoder

	// GRPCWebDecoder extracts Protobuf payloads from 5-byte gRPC-Web frames and validates trailers.
	GRPCWebDecoder = decode.GRPCWebDecoder

	// ProtoJSONDecoder parses JSON response streams into Protobuf messages via protojson.
	ProtoJSONDecoder = decode.ProtoJSONDecoder

	// RawDecoder reads the entire response payload stream directly into a byte slice pointer (*[]byte).
	RawDecoder = decode.RawDecoder
)

// To unmarshals the response payload stream from reader into a newly allocated instance of T using decoder.
func To[T any](reader io.Reader, decoder Decoder) (T, error) {
	return decode.To[T](reader, decoder)
}

// ToResult unmarshals the response stream from reader into a Swift-inspired [generic.Result].
func ToResult[T any](reader io.Reader, decoder Decoder) generic.Result[T] {
	return decode.ToResult[T](reader, decoder)
}

// Result unmarshals the response stream from reader into a Swift-inspired [generic.Result].
func Result[T any](reader io.Reader, decoder Decoder) generic.Result[T] {
	return decode.ToResult[T](reader, decoder)
}

// Payload decodes rawBody into target based on contentType using auto-matched or default decoders.
func Payload(contentType string, rawBody []byte, target any) error {
	return decode.Payload(contentType, rawBody, target)
}

// JSON parses response stream bytes into a typed instance of T.
func JSON[T any](reader io.Reader) (T, error) {
	return decode.JSON[T](reader)
}

// XML parses XML response stream bytes into a typed instance of T.
func XML[T any](reader io.Reader) (T, error) {
	return decode.XML[T](reader)
}

// YAML parses YAML response stream bytes into a typed instance of T.
func YAML[T any](reader io.Reader) (T, error) {
	return decode.YAML[T](reader)
}

// Proto parses binary Protocol Buffer stream bytes into a typed [proto.Message] instance.
func Proto[T any](reader io.Reader) (T, error) {
	return decode.Proto[T](reader)
}

// GRPCWeb extracts and parses Protobuf payloads from 5-byte gRPC-Web framed streams.
func GRPCWeb[T any](reader io.Reader) (T, error) {
	return decode.GRPCWeb[T](reader)
}

// ProtoJSON parses JSON response streams into typed Protobuf messages via protojson.
func ProtoJSON[T any](reader io.Reader) (T, error) {
	return decode.ProtoJSON[T](reader)
}

// Raw reads the entire response stream into a raw byte slice.
func Raw(reader io.Reader) ([]byte, error) {
	return decode.Raw(reader)
}

// Encode encodes a struct into [url.Values] using `url` struct tags.
var Encode = values.Encode

// StructToQueryString converts a struct into a URL-encoded query parameter string.
var StructToQueryString = values.StructToQueryString
