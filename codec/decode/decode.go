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
	stdio "io"
	"reflect"
	"strings"
	"sync"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/mod"
)

var bufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

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

// JSONDecoderConfig configures parsing options for JSON response streams.
type JSONDecoderConfig struct {
	DisallowUnknownFields bool
	UseNumber             bool
}

type customJSONDecoder struct {
	cfg JSONDecoderConfig
}

func (d customJSONDecoder) Decode(reader stdio.Reader, target any) error {
	dec := json.NewDecoder(reader)
	if d.cfg.DisallowUnknownFields {
		dec.DisallowUnknownFields()
	}

	if d.cfg.UseNumber {
		dec.UseNumber()
	}

	return dec.Decode(target)
}

// NewJSONDecoder creates a custom JSON [Decoder] with the specified configuration parameters.
func NewJSONDecoder(cfg JSONDecoderConfig) Decoder {
	return customJSONDecoder{cfg: cfg}
}

type jsonDecoder struct{}

func (jsonDecoder) Decode(reader stdio.Reader, target any) error {
	return json.NewDecoder(reader).Decode(target)
}

type xmlDecoder struct{}

func (xmlDecoder) Decode(reader stdio.Reader, target any) error {
	return xml.NewDecoder(StripBOM(reader)).Decode(target)
}

type rawDecoder struct{}

func (rawDecoder) Decode(r stdio.Reader, target any) error {
	outPtr, ok := target.(*[]byte)
	if !ok {
		return &Error{Format: "raw", Target: typeName(target), Err: ErrInvalidRawTarget}
	}

	rawBytes, err := stdio.ReadAll(r)
	if err != nil {
		return &Error{Format: "raw", Target: typeName(target), Err: err}
	}

	*outPtr = rawBytes

	return nil
}

type protoDecoder struct{}

func (protoDecoder) Decode(r stdio.Reader, target any) error {
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

func (protoJSONDecoder) Decode(r stdio.Reader, target any) error {
	msg, err := castOrResolveProto(target)
	if err != nil {
		return err
	}

	buf, err := copyToBuffer(r)
	if err != nil {
		return err
	}
	defer bufferPool.Put(buf)

	opts := protojson.UnmarshalOptions{DiscardUnknown: true}

	if err := opts.Unmarshal(buf.Bytes(), msg); err != nil {
		return &Error{Format: "protojson", Target: typeName(msg), Err: err}
	}

	return nil
}

type grpcWebDecoder struct{}

func (grpcWebDecoder) Decode(r stdio.Reader, target any) error {
	msg, err := castOrResolveProto(target)
	if err != nil {
		return err
	}

	br := bufio.NewReader(r)

	var reader stdio.Reader = br
	if peek, err := br.Peek(5); err == nil && IsBase64Header(peek) {
		reader = base64.NewDecoder(base64.StdEncoding, br)
	}

	return readGRPCWebFrames(reader, msg)
}

func readGRPCWebFrames(reader stdio.Reader, msg proto.Message) error {
	var (
		header      [5]byte
		payloadRead bool
	)

	for {
		if _, err := stdio.ReadFull(reader, header[:]); err != nil {
			if (errors.Is(err, stdio.EOF) || errors.Is(err, stdio.ErrUnexpectedEOF)) && payloadRead {
				return nil
			}

			return &GRPCWebError{Op: "read_header", Err: ErrInvalidGRPCWebFrame}
		}

		flags := header[0]
		length := binary.BigEndian.Uint32(header[1:5])

		payload := make([]byte, length)
		if _, err := stdio.ReadFull(reader, payload); err != nil {
			return &GRPCWebError{Op: "read_payload", Err: ErrInvalidGRPCWebFrame}
		}

		done, err := processGRPCWebFrame(flags, payload, msg)
		if err != nil {
			return err
		}

		if done {
			return nil
		}

		payloadRead = true
	}
}

func processGRPCWebFrame(flags byte, payload []byte, msg proto.Message) (done bool, err error) {
	if flags&0x80 != 0 {
		if err := verifyGRPCTrailer(payload); err != nil {
			return false, err
		}

		return true, nil
	}

	if flags&0x01 != 0 {
		var err error

		payload, err = decompressProtoPayload(payload)
		if err != nil {
			return false, err
		}
	}

	if err := proto.Unmarshal(payload, msg); err != nil {
		return false, &Error{Format: "grpc-web", Target: typeName(msg), Err: err}
	}

	return false, nil
}

func copyToBuffer(r stdio.Reader) (*bytes.Buffer, error) {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()

	if _, err := io.CopyZeroAlloc(buf, r); err != nil {
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

	decompressed, err := stdio.ReadAll(gzReader)
	_ = gzReader.Close()

	if err != nil {
		return nil, &GRPCWebError{Op: "read_decompressed", Err: err}
	}

	return decompressed, nil
}

func verifyGRPCTrailer(trailerPayload []byte) error {
	var statusCode, statusMsg string

	for len(trailerPayload) > 0 {
		var line []byte

		if idx := bytes.IndexByte(trailerPayload, '\n'); idx >= 0 {
			line = trailerPayload[:idx]
			trailerPayload = trailerPayload[idx+1:]
		} else {
			line = trailerPayload
			trailerPayload = nil
		}

		key, val, ok := parseTrailerKeyValue(line)
		if !ok {
			continue
		}

		if bytesconv.EqualFoldASCII(key, "grpc-status") {
			statusCode = val
		} else if bytesconv.EqualFoldASCII(key, "grpc-message") {
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

func parseTrailerKeyValue(line []byte) (key, val string, ok bool) {
	keyBytes, valBytes, ok := bytes.Cut(line, []byte{':'})
	if !ok {
		return "", "", false
	}

	key = strings.TrimSpace(bytesconv.B2S(keyBytes))
	val = strings.TrimSpace(bytesconv.B2S(valBytes))

	return key, val, true
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

// ByContentType selects a registered decoder matching the MIME type in contentType.
func ByContentType(reader stdio.Reader, contentType string, target any) error {
	mediaType, _, _ := strings.Cut(contentType, ";")
	mediaType = strings.TrimSpace(mediaType)

	switch {
	case bytesconv.EqualFoldASCII(mediaType, "application/json"), bytesconv.EqualFoldASCII(mediaType, "text/json"):
		return JSONDecoder.Decode(reader, target)
	case bytesconv.EqualFoldASCII(mediaType, "application/x-protobuf"),
		bytesconv.EqualFoldASCII(mediaType, "application/protobuf"):
		return ProtoDecoder.Decode(reader, target)
	case bytesconv.EqualFoldASCII(mediaType, "application/grpc-web+proto"),
		bytesconv.EqualFoldASCII(mediaType, "application/grpc-web"),
		bytesconv.EqualFoldASCII(mediaType, "application/grpc-web-text"):
		return GRPCWebDecoder.Decode(reader, target)
	case bytesconv.EqualFoldASCII(mediaType, "application/xml"), bytesconv.EqualFoldASCII(mediaType, "text/xml"):
		return XMLDecoder.Decode(reader, target)
	default:
		return RawDecoder.Decode(reader, target)
	}
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

// StripBOM strips UTF-8 and UTF-16 Byte Order Marks from the input stream.
func StripBOM(reader stdio.Reader) stdio.Reader {
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

	n, _ := stdio.ReadFull(reader, buf[:])
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

		return stdio.MultiReader(bytes.NewReader(unread), reader)
	}

	return stdio.MultiReader(bytes.NewReader(buf[:n]), reader)
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
	return verifyGRPCTrailer(trailerPayload)
}

// IsBase64Header checks whether frame prefix matches Base64 text-encoded gRPC-Web stream.
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
