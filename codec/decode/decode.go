// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
)

var (
	bytePool = sync.Pool{
		New: func() any {
			b := make([]byte, 32*1024)
			return &b
		},
	}
	bufferPool = sync.Pool{
		New: func() any {
			return new(bytes.Buffer)
		},
	}
)

// RawDecoder reads the entire response stream directly into a byte slice.
// The destination target must be of type *[]byte.
var RawDecoder Decoder = rawDecoder{}

// ProtoDecoder reads raw binary Protocol Buffer payloads into proto.Message targets.
var ProtoDecoder Decoder = protoDecoder{}

// GRPCWebDecoder extracts Protobuf payloads from 5-byte gRPC-Web frames and validates server trailers.
var GRPCWebDecoder Decoder = grpcWebDecoder{}

// JSONDecoder parses the response stream as standard JSON into the target structure.
var JSONDecoder Decoder = jsonDecoder{}

// XMLDecoder parses the response stream as XML into the target structure.
var XMLDecoder Decoder = xmlDecoder{}

// ProtoJSONDecoder parses JSON response streams into Protobuf messages using standard protojson mapping.
var ProtoJSONDecoder Decoder = protoJSONDecoder{}

// Decoder defines the contract for unmarshaling response payload streams into Go structures.
type Decoder interface {
	// Decode reads from reader and unmarshals the content into target.
	Decode(reader io.Reader, target any) error
}

// DecoderFunc adapts a function signature to satisfy the Decoder interface.
type DecoderFunc func(reader io.Reader, target any) error

// Decode executes the underlying function to parse reader data into target.
func (f DecoderFunc) Decode(reader io.Reader, target any) error {
	return f(reader, target)
}

// JSONDecoderConfig configures custom behavior for JSON payload decoding.
type JSONDecoderConfig struct {
	// DisallowUnknownFields causes decoding to fail if the JSON payload contains key names
	// not matching any struct fields in the target.
	DisallowUnknownFields bool

	// UseNumber unmarshals JSON numbers into json.Number instead of float64.
	UseNumber bool
}

type customJSONDecoder struct {
	cfg JSONDecoderConfig
}

func (d customJSONDecoder) Decode(reader io.Reader, target any) error {
	dec := json.NewDecoder(StripBOM(reader))
	if d.cfg.DisallowUnknownFields {
		dec.DisallowUnknownFields()
	}

	if d.cfg.UseNumber {
		dec.UseNumber()
	}

	return dec.Decode(target)
}

// NewJSONDecoder creates a custom JSON [Decoder] configured with the specified parameters.
func NewJSONDecoder(cfg JSONDecoderConfig) Decoder {
	return customJSONDecoder{cfg: cfg}
}

type jsonDecoder struct{}

func (jsonDecoder) Decode(reader io.Reader, target any) error {
	return json.NewDecoder(StripBOM(reader)).Decode(target)
}

type xmlDecoder struct{}

func (xmlDecoder) Decode(reader io.Reader, target any) error {
	return xml.NewDecoder(StripBOM(reader)).Decode(target)
}

type rawDecoder struct{}

func (rawDecoder) Decode(r io.Reader, target any) error {
	outPtr, ok := target.(*[]byte)
	if !ok {
		return &Error{Format: "raw", Target: typeName(target), Err: ErrInvalidRawTarget}
	}

	buf, err := copyToBuffer(r)
	if err != nil {
		return err
	}
	defer bufferPool.Put(buf)

	*outPtr = bytes.Clone(buf.Bytes())

	return nil
}

type protoDecoder struct{}

func (protoDecoder) Decode(r io.Reader, target any) error {
	msg, err := castOrResolveProto(target)
	if err != nil {
		return err
	}

	buf, err := copyToBuffer(r)
	if err != nil {
		return err
	}
	defer bufferPool.Put(buf)

	if err := proto.Unmarshal(buf.Bytes(), msg); err != nil {
		return &Error{Format: "proto", Target: typeName(msg), Err: err}
	}

	return nil
}

type protoJSONDecoder struct{}

func (protoJSONDecoder) Decode(r io.Reader, target any) error {
	msg, err := castOrResolveProto(target)
	if err != nil {
		return err
	}

	buf, err := copyToBuffer(r)
	if err != nil {
		return err
	}
	defer bufferPool.Put(buf)

	opts := protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}

	if err := opts.Unmarshal(buf.Bytes(), msg); err != nil {
		return &Error{Format: "protojson", Target: typeName(msg), Err: err}
	}

	return nil
}

type grpcWebDecoder struct{}

func (grpcWebDecoder) Decode(r io.Reader, target any) error {
	msg, err := castOrResolveProto(target)
	if err != nil {
		return err
	}

	br := bufio.NewReader(r)
	if peek, err := br.Peek(5); err == nil && IsBase64Header(peek) {
		r = base64.NewDecoder(base64.StdEncoding, br)
	} else {
		r = br
	}

	var (
		header      [5]byte
		payloadRead bool
	)

	for {
		if _, err := io.ReadFull(r, header[:]); err != nil {
			if (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) && payloadRead {
				return nil
			}

			return &GRPCWebError{Op: "read_header", Err: ErrInvalidGRPCWebFrame}
		}

		flags := header[0]
		length := binary.BigEndian.Uint32(header[1:5])

		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return &GRPCWebError{Op: "read_payload", Err: ErrInvalidGRPCWebFrame}
		}

		if flags&0x80 != 0 {
			if err := verifyGRPCTrailer(payload); err != nil {
				return err
			}

			continue
		}

		if flags&0x01 != 0 {
			payload, err = decompressProtoPayload(payload)
			if err != nil {
				return err
			}
		}

		if err := proto.Unmarshal(payload, msg); err != nil {
			return &Error{Format: "grpc-web", Target: typeName(msg), Err: err}
		}

		payloadRead = true
	}
}

func copyToBuffer(r io.Reader) (*bytes.Buffer, error) {
	buf := bufferPool.Get().(*bytes.Buffer)

	buf.Reset()

	bufPtr := bytePool.Get().(*[]byte)
	defer bytePool.Put(bufPtr)

	if _, err := io.CopyBuffer(buf, r, *bufPtr); err != nil {
		bufferPool.Put(buf)
		return nil, err
	}

	return buf, nil
}

func decompressProtoPayload(payload []byte) ([]byte, error) {
	gzReader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, &GRPCWebError{Op: "decompress", Err: err}
	}

	decompressed, err := io.ReadAll(gzReader)
	_ = gzReader.Close()

	if err != nil {
		return nil, &GRPCWebError{Op: "read_decompressed", Err: err}
	}

	return decompressed, nil
}

func verifyGRPCTrailer(trailerPayload []byte) error {
	scanner := bufio.NewScanner(bytes.NewReader(trailerPayload))

	var statusCode, statusMsg string

	for scanner.Scan() {
		line := scanner.Text()

		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}

		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)

		switch key {
		case "grpc-status":
			statusCode = val
		case "grpc-message":
			statusMsg = val
		}
	}

	if statusCode != "" && statusCode != "0" {
		return &GRPCWebError{
			StatusCode: statusCode,
			StatusMsg:  statusMsg,
			Err:        ErrGRPCWebStatusError,
		}
	}

	return nil
}

func castOrResolveProto(target any) (proto.Message, error) {
	if msg, ok := target.(proto.Message); ok {
		return msg, nil
	}

	val := reflect.ValueOf(target)
	if val.Kind() == reflect.Pointer && !val.IsNil() {
		elem := val.Elem()
		if elem.Kind() == reflect.Pointer && elem.IsNil() && elem.CanSet() {
			elem.Set(reflect.New(elem.Type().Elem()))

			if msg, ok := elem.Interface().(proto.Message); ok {
				return msg, nil
			}
		}
	}

	return nil, &Error{Format: "proto", Target: typeName(target), Err: ErrInvalidProtoTarget}
}

type limitDecoder struct {
	decoder  Decoder
	maxBytes int64
}

func (l limitDecoder) Decode(reader io.Reader, target any) error {
	return l.decoder.Decode(io.LimitReader(reader, l.maxBytes), target)
}

// LimitDecoder wraps an existing decoder to cap input stream consumption at maxBytes.
func LimitDecoder(decoder Decoder, maxBytes int64) Decoder {
	return limitDecoder{
		decoder:  decoder,
		maxBytes: maxBytes,
	}
}

// ByContentType automatically inspects the MIME type in contentType and selects
// a matching registered decoder. Defaults to RawDecoder if unrecognized.
func ByContentType(reader io.Reader, contentType string, target any) error {
	mediaType, _, _ := strings.Cut(contentType, ";")
	mediaType = strings.TrimSpace(strings.ToLower(mediaType))

	switch mediaType {
	case "application/json", "text/json":
		return JSONDecoder.Decode(reader, target)
	case "application/x-protobuf", "application/protobuf":
		return ProtoDecoder.Decode(reader, target)
	case "application/grpc-web+proto", "application/grpc-web", "application/grpc-web-text":
		return GRPCWebDecoder.Decode(reader, target)
	case "application/xml", "text/xml":
		return XMLDecoder.Decode(reader, target)
	default:
		return RawDecoder.Decode(reader, target)
	}
}

// To allocates a new instance of type T and decodes reader data into it using the specified decoder.
func To[T any](reader io.Reader, decoder Decoder) (T, error) {
	var target T
	if err := decoder.Decode(reader, &target); err != nil {
		var zero T
		return zero, err
	}

	return target, nil
}

// IsRawDecoder reports whether the provided decoder is the raw byte-slice decoder.
func IsRawDecoder(decoder Decoder) bool {
	_, ok := decoder.(rawDecoder)
	return ok
}

// JSON reads from reader and unmarshals JSON data into T.
func JSON[T any](reader io.Reader) (T, error) {
	return To[T](reader, JSONDecoder)
}

// XML reads from reader and unmarshals XML data into T.
func XML[T any](reader io.Reader) (T, error) {
	return To[T](reader, XMLDecoder)
}

// Proto reads from reader and unmarshals binary Protocol Buffer data into T.
func Proto[T any](reader io.Reader) (T, error) {
	return To[T](reader, ProtoDecoder)
}

// GRPCWeb reads from reader and unmarshals gRPC-Web framed Protocol Buffer data into T.
func GRPCWeb[T any](reader io.Reader) (T, error) {
	return To[T](reader, GRPCWebDecoder)
}

// WithRaw creates a request modifier that configures the request to use RawDecoder.
func WithRaw() aoni.RequestModifier { return mod.WithDecoder(RawDecoder) }

// WithJSON creates a request modifier that configures the request to use JSONDecoder.
func WithJSON() aoni.RequestModifier { return mod.WithDecoder(JSONDecoder) }

// WithXML creates a request modifier that configures the request to use XMLDecoder.
func WithXML() aoni.RequestModifier { return mod.WithDecoder(XMLDecoder) }

// WithProto creates a request modifier that sets ProtoDecoder for response parsing.
func WithProto() aoni.RequestModifier { return mod.WithDecoder(ProtoDecoder) }

// WithGRPCWeb creates a request modifier that sets GRPCWebDecoder for response parsing.
func WithGRPCWeb() aoni.RequestModifier { return mod.WithDecoder(GRPCWebDecoder) }

// StripBOM removes the BOM (Byte Order Mark) from the reader if present without allocations.
func StripBOM(reader io.Reader) io.Reader {
	var br *bufio.Reader
	switch r := reader.(type) {
	case *bufio.Reader:
		br = r
	case interface{ BufioReader() *bufio.Reader }:
		br = r.BufioReader()
	}

	if br != nil {
		return stripBufferBOM(br)
	}

	var buf [3]byte

	n, _ := io.ReadFull(reader, buf[:])
	if n == 0 {
		return reader
	}

	if n >= 3 && buf[0] == 0xEF && buf[1] == 0xBB && buf[2] == 0xBF {
		return reader
	}

	if n >= 2 && ((buf[0] == 0xFE && buf[1] == 0xFF) || (buf[0] == 0xFF && buf[1] == 0xFE)) {
		unread := buf[2:n]
		if len(unread) == 0 {
			return reader
		}

		return io.MultiReader(bytes.NewReader(unread), reader)
	}

	return io.MultiReader(bytes.NewReader(buf[:n]), reader)
}

func stripBufferBOM(br *bufio.Reader) *bufio.Reader {
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

// VerifyGRPCTrailer parses gRPC-Web trailer key-value headers and validates grpc-status codes.
func VerifyGRPCTrailer(trailerPayload []byte) error {
	scanner := bufio.NewScanner(bytes.NewReader(trailerPayload))

	var statusCode, statusMsg string

	for scanner.Scan() {
		line := scanner.Text()

		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}

		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)

		switch key {
		case "grpc-status":
			statusCode = val
		case "grpc-message":
			statusMsg = val
		}
	}

	if statusCode != "" && statusCode != "0" {
		return &GRPCWebError{
			StatusCode: statusCode,
			StatusMsg:  statusMsg,
			Err:        ErrGRPCWebStatusError,
		}
	}

	return nil
}

// IsBase64Header checks whether the frame prefix matches a Base64 text-encoded gRPC-Web stream.
func IsBase64Header(header []byte) bool {
	if len(header) < 5 {
		return false
	}

	first := header[0]

	return (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z') || (first >= '0' && first <= '9')
}

func typeName(target any) string {
	if target == nil {
		return "<nil>"
	}

	return reflect.TypeOf(target).String()
}
