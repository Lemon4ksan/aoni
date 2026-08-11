// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import (
	"bufio"
	"bytes"
	stdio "io"
	"reflect"
	"strings"
	"sync"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
	"github.com/lemon4ksan/aoni/mod"
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

	// ProtoJSONDecoder parses JSON response streams into Protobuf messages via protojson.
	ProtoJSONDecoder Decoder = protoJSONDecoder{}
)

// Decoder defines the contract for unmarshaling response payload streams into Go structures.
type Decoder interface {
	Decode(reader stdio.Reader, target any) error
}

// DecoderFunc adapts a plain function signature to satisfy the [Decoder] interface.
type DecoderFunc func(reader stdio.Reader, target any) error

// Decode executes the underlying function to parse reader data into target.
func (f DecoderFunc) Decode(reader stdio.Reader, target any) error {
	return f(reader, target)
}

type limitDecoder struct {
	decoder  Decoder
	maxBytes int64
}

func (l limitDecoder) Decode(reader stdio.Reader, target any) error {
	return l.decoder.Decode(stdio.LimitReader(reader, l.maxBytes), target)
}

// LimitDecoder caps response payload input stream consumption at maxBytes.
func LimitDecoder(decoder Decoder, maxBytes int64) Decoder {
	return limitDecoder{
		decoder:  decoder,
		maxBytes: maxBytes,
	}
}

var (
	decodersMu         sync.RWMutex
	registeredDecoders = make(map[string]Decoder)
)

func normalizeContentType(contentType string) string {
	mediaType, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(mediaType))
}

// RegisterDecoder registers a custom [Decoder] globally for a MIME content type (e.g. "application/x-msgpack").
func RegisterDecoder(contentType string, decoder Decoder) {
	norm := normalizeContentType(contentType)
	if norm == "" {
		return
	}

	decodersMu.Lock()
	defer decodersMu.Unlock()

	if decoder == nil {
		delete(registeredDecoders, norm)
	} else {
		registeredDecoders[norm] = decoder
	}
}

// UnregisterDecoder removes a custom [Decoder] globally registered for a MIME content type.
func UnregisterDecoder(contentType string) {
	RegisterDecoder(contentType, nil)
}

// GetDecoder retrieves the custom [Decoder] globally registered for contentType, or nil if none is registered.
func GetDecoder(contentType string) Decoder {
	norm := normalizeContentType(contentType)
	if norm == "" {
		return nil
	}

	decodersMu.RLock()
	defer decodersMu.RUnlock()

	return registeredDecoders[norm]
}

// LookupDecoder resolves a [Decoder] for contentType, checking registered custom decoders first,
// then standard MIME types (JSON, Proto, gRPC-Web, XML), falling back to RawDecoder.
func LookupDecoder(contentType string) Decoder {
	norm := normalizeContentType(contentType)
	if norm != "" {
		decodersMu.RLock()

		d, ok := registeredDecoders[norm]

		decodersMu.RUnlock()

		if ok {
			return d
		}
	}

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
	default:
		return RawDecoder
	}
}

// ByContentType selects a registered decoder matching the MIME type in contentType.
func ByContentType(reader stdio.Reader, contentType string, target any) error {
	return LookupDecoder(contentType).Decode(reader, target)
}

// To allocates a new instance of T and decodes payload data into it.
func To[T any](reader stdio.Reader, decoder Decoder) (T, error) {
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
func StripBOM(reader stdio.Reader) stdio.Reader {
	br, ok := reader.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(reader)
	}

	peek, err := br.Peek(3)
	if err == nil && len(peek) >= 3 && bytes.HasPrefix(peek, []byte{0xEF, 0xBB, 0xBF}) {
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
func JSON[T any](reader stdio.Reader) (T, error) {
	return To[T](reader, JSONDecoder)
}

// XML reads from reader and unmarshals XML data into a newly allocated T.
func XML[T any](reader stdio.Reader) (T, error) {
	return To[T](reader, XMLDecoder)
}

// Proto reads from reader and unmarshals binary Protocol Buffer data into a newly allocated T.
func Proto[T any](reader stdio.Reader) (T, error) {
	return To[T](reader, ProtoDecoder)
}

// GRPCWeb reads from reader and unmarshals gRPC-Web framed data into a newly allocated T.
func GRPCWeb[T any](reader stdio.Reader) (T, error) {
	return To[T](reader, GRPCWebDecoder)
}

// WithRaw creates an [aoni.RequestModifier] that assigns [RawDecoder] for response parsing.
func WithRaw() aoni.RequestModifier { return mod.WithDecoder(RawDecoder) }

// WithJSON creates an [aoni.RequestModifier] that assigns [JSONDecoder] for response parsing.
func WithJSON() aoni.RequestModifier { return mod.WithDecoder(JSONDecoder) }

// WithXML creates an [aoni.RequestModifier] that assigns [XMLDecoder] for response parsing.
func WithXML() aoni.RequestModifier { return mod.WithDecoder(XMLDecoder) }

// WithProto creates an [aoni.RequestModifier] that assigns [ProtoDecoder] for response parsing.
func WithProto() aoni.RequestModifier { return mod.WithDecoder(ProtoDecoder) }

// WithGRPCWeb creates an [aoni.RequestModifier] that assigns [GRPCWebDecoder] for response parsing.
func WithGRPCWeb() aoni.RequestModifier { return mod.WithDecoder(GRPCWebDecoder) }

func typeName(target any) string {
	if target == nil {
		return "<nil>"
	}

	return reflect.TypeOf(target).String()
}

// DecodePayload decodes rawBody into target based on contentType using auto-matched or default decoders.
func DecodePayload(contentType string, rawBody []byte, target any) error {
	if target == nil {
		return nil
	}

	decoder := LookupDecoder(contentType)

	return decoder.Decode(bytes.NewReader(rawBody), target)
}
