// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import (
	"bufio"
	"bytes"
	"io"
	"reflect"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/pipeline"
)

var (
	// RawDecoder reads the entire response payload stream directly into a byte slice pointer (*[]byte).
	RawDecoder Decoder = rawDecoder{}

	// ProtoDecoder reads binary Protocol Buffer payloads into [proto.Message] targets.
	ProtoDecoder Decoder = protoDecoder{}

	// GRPCWebDecoder extracts Protobuf payloads from 5-byte gRPC-Web frames and validates trailers.
	GRPCWebDecoder Decoder = grpcWebDecoder{}

	// JSONDecoder parses response payload streams as standard JSON.
	JSONDecoder Decoder = jsonDecoder{}

	// XMLDecoder parses response payload streams as XML.
	XMLDecoder Decoder = xmlDecoder{}

	// YAMLDecoder parses response payload streams as YAML.
	YAMLDecoder Decoder = yamlDecoder{}

	// ProtoJSONDecoder parses JSON response streams into Protobuf messages via protojson.
	ProtoJSONDecoder Decoder = protoJSONDecoder{}
)

// Decoder defines the contract for unmarshaling response payload streams into Go structures.
type Decoder interface {
	Decode(reader io.Reader, target any) error
}

// DecodeTo decodes the payload from reader into a newly allocated instance of Target.
func DecodeTo[Target any](d Decoder, reader io.Reader) (Target, error) {
	var target Target

	err := d.Decode(reader, &target)

	return target, err
}

// DecodeResult decodes the payload from reader into a Swift-inspired [generic.Result].
func DecodeResult[Target any](d Decoder, reader io.Reader) generic.Result[Target] {
	var target Target
	if err := d.Decode(reader, &target); err != nil {
		return generic.Failure[Target](err)
	}

	return generic.Success(target)
}

// Result decodes the payload from reader into a Swift-inspired [generic.Result] using d.
func Result[Target any](reader io.Reader, d Decoder) generic.Result[Target] {
	return DecodeResult[Target](d, reader)
}

// DecoderFunc adapts a plain function signature to satisfy the [Decoder] interface.
type DecoderFunc func(reader io.Reader, target any) error

// Decode executes the underlying function to parse reader data into target.
func (f DecoderFunc) Decode(reader io.Reader, target any) error {
	return f(reader, target)
}

type limitDecoder struct {
	decoder  Decoder
	maxBytes int64
}

func (l limitDecoder) Decode(reader io.Reader, target any) error {
	return l.decoder.Decode(io.LimitReader(reader, l.maxBytes), target)
}

// LimitDecoder caps response payload input stream consumption at maxBytes.
func LimitDecoder(decoder Decoder, maxBytes int64) Decoder {
	return limitDecoder{
		decoder:  decoder,
		maxBytes: maxBytes,
	}
}

var bomUTF8 = []byte{0xEF, 0xBB, 0xBF}

// normalizeContentType extracts the media type from a Content-Type header string (e.g. "application/json; charset=utf-8" -> "application/json").
func normalizeContentType(contentType string) string {
	mediaType, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(mediaType))
}

// LookupDecoder resolves a standard [Decoder] matching the provided MIME content type,
// falling back to [RawDecoder] if unsupported.
func LookupDecoder(contentType string) Decoder {
	norm := normalizeContentType(contentType)

	switch {
	case bytesconv.EqualFoldASCII(norm, "application/json"), bytesconv.EqualFoldASCII(norm, "text/json"):
		return JSONDecoder
	case bytesconv.EqualFoldASCII(norm, "application/x-protobuf"),
		bytesconv.EqualFoldASCII(norm, "application/protobuf"):
		return ProtoDecoder
	case bytesconv.EqualFoldASCII(norm, "application/grpc-web+proto"),
		bytesconv.EqualFoldASCII(norm, "application/grpc-web"),
		bytesconv.EqualFoldASCII(norm, "application/grpc-web-text"):
		return GRPCWebDecoder
	case bytesconv.EqualFoldASCII(norm, "application/xml"), bytesconv.EqualFoldASCII(norm, "text/xml"):
		return XMLDecoder
	case bytesconv.EqualFoldASCII(norm, "application/x-yaml"),
		bytesconv.EqualFoldASCII(norm, "application/yaml"),
		bytesconv.EqualFoldASCII(norm, "text/x-yaml"),
		bytesconv.EqualFoldASCII(norm, "text/yaml"):
		return YAMLDecoder
	default:
		return RawDecoder
	}
}

// ByContentType selects a registered decoder matching the MIME type in contentType.
func ByContentType(reader io.Reader, contentType string, target any) error {
	return LookupDecoder(contentType).Decode(reader, target)
}

// To allocates a new instance of T and decodes payload data into it.
func To[T any](reader io.Reader, decoder Decoder) (T, error) {
	var target T
	if err := decoder.Decode(reader, &target); err != nil {
		var zero T
		return zero, err
	}

	return target, nil
}

// IsRawDecoder reports whether decoder is the raw byte-slice decoder.
func IsRawDecoder(decoder Decoder) bool {
	_, ok := decoder.(rawDecoder)
	return ok
}

// StripBOM detects and discards UTF-8, UTF-16LE, and UTF-16BE Byte Order Marks (BOM) from reader.
func StripBOM(reader io.Reader) io.Reader {
	br, ok := reader.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(reader)
	}

	peek, err := br.Peek(3)
	if err == nil && len(peek) >= 3 && bytes.HasPrefix(peek, bomUTF8) {
		_, _ = br.Discard(3)
		return br
	}

	peek, err = br.Peek(2)
	if err == nil && len(peek) >= 2 {
		if (peek[0] == 0xFE && peek[1] == 0xFF) || (peek[0] == 0xFF && peek[1] == 0xFE) {
			_, _ = br.Discard(2)
		}
	}

	return br
}

// JSON reads from reader and unmarshals JSON data into a newly allocated T.
func JSON[T any](reader io.Reader) (T, error) {
	return To[T](reader, JSONDecoder)
}

// XML reads from reader and unmarshals XML data into a newly allocated T.
func XML[T any](reader io.Reader) (T, error) {
	return To[T](reader, XMLDecoder)
}

// YAML reads from reader and unmarshals YAML data into a newly allocated T.
func YAML[T any](reader io.Reader) (T, error) {
	return To[T](reader, YAMLDecoder)
}

// Proto reads from reader and unmarshals binary Protocol Buffer data into a newly allocated T.
func Proto[T any](reader io.Reader) (T, error) {
	return To[T](reader, ProtoDecoder)
}

// GRPCWeb reads from reader and unmarshals gRPC-Web framed data into a newly allocated T.
func GRPCWeb[T any](reader io.Reader) (T, error) {
	return To[T](reader, GRPCWebDecoder)
}

// ProtoJSON reads from reader and unmarshals JSON data into a newly allocated Protobuf message T.
func ProtoJSON[T any](reader io.Reader) (T, error) {
	return To[T](reader, ProtoJSONDecoder)
}

// Raw reads the entire response stream into a raw byte slice.
func Raw(reader io.Reader) ([]byte, error) {
	var target []byte

	err := RawDecoder.Decode(reader, &target)

	return target, err
}

// WithRaw creates an [core.RequestModifier] that assigns [RawDecoder] for response parsing.
func WithRaw() core.RequestModifier {
	return core.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req core.Request) {
			pipeline.GetOrInitRequestConfig(req).Decoder = RawDecoder
		},
	}
}

// WithJSON creates an [core.RequestModifier] that assigns [JSONDecoder] for response parsing.
func WithJSON() core.RequestModifier {
	return core.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req core.Request) {
			pipeline.GetOrInitRequestConfig(req).Decoder = JSONDecoder
		},
	}
}

// WithXML creates an [core.RequestModifier] that assigns [XMLDecoder] for response parsing.
func WithXML() core.RequestModifier {
	return core.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req core.Request) {
			pipeline.GetOrInitRequestConfig(req).Decoder = XMLDecoder
		},
	}
}

// WithYAML creates an [core.RequestModifier] that assigns [YAMLDecoder] for response parsing.
func WithYAML() core.RequestModifier {
	return core.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req core.Request) {
			pipeline.GetOrInitRequestConfig(req).Decoder = YAMLDecoder
		},
	}
}

// WithProto creates an [core.RequestModifier] that assigns [ProtoDecoder] for response parsing.
func WithProto() core.RequestModifier {
	return core.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req core.Request) {
			pipeline.GetOrInitRequestConfig(req).Decoder = ProtoDecoder
		},
	}
}

// WithGRPCWeb creates an [core.RequestModifier] that assigns [GRPCWebDecoder] for response parsing.
func WithGRPCWeb() core.RequestModifier {
	return core.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req core.Request) {
			pipeline.GetOrInitRequestConfig(req).Decoder = GRPCWebDecoder
		},
	}
}

// typeName extracts a string representation of target's concrete type for error reporting.
func typeName(target any) string {
	if target == nil {
		return "<nil>"
	}

	return reflect.TypeOf(target).String()
}

// DecodePayload decodes rawBody into target based on contentType using auto-matched or default decoders.
// Thread-safe for concurrent execution.
func DecodePayload(contentType string, rawBody []byte, target any) error {
	if target == nil {
		return nil
	}

	decoder := LookupDecoder(contentType)

	return decoder.Decode(bytes.NewReader(rawBody), target)
}

// UnmarshalJSON parses JSON bytes into target using [JSONDecoder].
func UnmarshalJSON(data []byte, target any) error {
	return JSONDecoder.Decode(bytes.NewReader(data), target)
}

// UnmarshalYAML parses YAML bytes into target using [YAMLDecoder].
func UnmarshalYAML(data []byte, target any) error {
	return YAMLDecoder.Decode(bytes.NewReader(data), target)
}
