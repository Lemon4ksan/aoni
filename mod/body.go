// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"io"
	"net/url"
	"strings"

	"github.com/lemon4ksan/foundation/codec/json"
	fheader "github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"

	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/pipeline"
)

// WithBody constructs an [RequestModifier] replacing the request body with the provided stream reader.
func WithBody(r io.Reader) RequestModifier {
	if r == nil {
		return RequestModifier{}
	}

	if b, ok := r.(*bytes.Buffer); ok {
		return RequestModifier{
			Kind:  core.ModBodyBytes,
			Bytes: b.Bytes(),
		}
	}

	if br, ok := r.(*bytes.Reader); ok {
		buf := make([]byte, br.Len())
		_, _ = br.ReadAt(buf, 0)

		return RequestModifier{
			Kind:  core.ModBodyBytes,
			Bytes: buf,
		}
	}

	if sr, ok := r.(*strings.Reader); ok {
		buf := make([]byte, sr.Len())
		_, _ = sr.ReadAt(buf, 0)

		return RequestModifier{
			Kind:  core.ModBodyBytes,
			Bytes: buf,
		}
	}

	return RequestModifier{
		Kind:   core.ModBodyStream,
		Stream: r,
	}
}

// WithBodyBytes constructs an [RequestModifier] setting raw byte slice payload directly as the request body.
func WithBodyBytes(b []byte) RequestModifier {
	return RequestModifier{
		Kind:  core.ModBodyBytes,
		Bytes: b,
	}
}

// WithJSONBody constructs an [RequestModifier] marshaling payload to JSON and setting Content-Type to application/json.
func WithJSONBody(payload any) RequestModifier {
	if payload == nil {
		return RequestModifier{}
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return RequestModifier{
			Kind: core.ModCustom,
			Fn: func(req Request) {
				getOrInitRequestConfig(req).BodyError = err
			},
		}
	}

	return RequestModifier{
		Kind:        core.ModBodyBytes,
		ContentType: fheader.MIMEApplicationJSON,
		Bytes:       bodyBytes,
	}
}

// WithJSON is a convenient alias for [WithJSONBody].
func WithJSON(payload any) RequestModifier {
	return WithJSONBody(payload)
}

// WithSmartBody constructs an [RequestModifier] that automatically detects the payload type:
//   - proto.Message -> Protobuf payload with application/x-protobuf
//   - url.Values -> URL-encoded form payload with application/x-www-form-urlencoded
//   - io.Reader -> Streamed request body
//   - []byte -> Raw byte slice payload
//   - string -> UTF-8 text payload with text/plain; charset=utf-8
//   - Struct / Map / Slice -> JSON-marshaled payload with application/json
func WithSmartBody(body any) RequestModifier {
	if body == nil {
		return RequestModifier{}
	}

	if mod, ok := body.(RequestModifier); ok {
		return mod
	}

	if msg, ok := body.(proto.Message); ok {
		return WithProtoBody(msg)
	}

	if uv, ok := body.(url.Values); ok {
		return WithFormValues(uv)
	}

	if r, ok := body.(io.Reader); ok {
		return WithBody(r)
	}

	if b, ok := body.([]byte); ok {
		return WithBodyBytes(b)
	}

	if s, ok := body.(string); ok {
		return RequestModifier{
			Kind:        core.ModBodyBytes,
			ContentType: fheader.MIMETextPlainCharsetUTF8,
			Bytes:       bytesconv.S2B(s),
		}
	}

	return WithJSONBody(body)
}

// WithXMLBody constructs an [RequestModifier] marshaling payload to XML and setting Content-Type to application/xml.
func WithXMLBody(payload any) RequestModifier {
	if payload == nil {
		return RequestModifier{}
	}

	bodyBytes, err := xml.Marshal(payload)
	if err != nil {
		return RequestModifier{
			Kind: core.ModCustom,
			Fn: func(req Request) {
				getOrInitRequestConfig(req).BodyError = err
			},
		}
	}

	return RequestModifier{
		Kind:        core.ModBodyBytes,
		ContentType: fheader.MIMEApplicationXML,
		Bytes:       bodyBytes,
	}
}

// WithYAMLBody constructs an [RequestModifier] marshaling payload to YAML and setting Content-Type to application/yaml.
func WithYAMLBody(payload any) RequestModifier {
	if payload == nil {
		return RequestModifier{}
	}

	bodyBytes, err := yaml.Marshal(payload)
	if err != nil {
		return RequestModifier{
			Kind: core.ModCustom,
			Fn: func(req Request) {
				getOrInitRequestConfig(req).BodyError = err
			},
		}
	}

	return RequestModifier{
		Kind:        core.ModBodyBytes,
		ContentType: fheader.MIMEApplicationYAML,
		Bytes:       bodyBytes,
	}
}

// WithProtoBody constructs an [RequestModifier] serializing a [proto.Message] into binary Protocol Buffer bytes.
func WithProtoBody(msg proto.Message) RequestModifier {
	if msg == nil {
		return RequestModifier{}
	}

	bodyBytes, err := proto.Marshal(msg)
	if err != nil {
		return RequestModifier{
			Kind: core.ModCustom,
			Fn: func(req Request) {
				getOrInitRequestConfig(req).BodyError = err
			},
		}
	}

	return RequestModifier{
		Kind:        core.ModBodyBytes,
		ContentType: fheader.MIMEApplicationProtobuf,
		Bytes:       bodyBytes,
	}
}

// WithGRPCWebBody constructs an [RequestModifier] serializing a [proto.Message] into 5-byte gRPC-Web framed bytes.
func WithGRPCWebBody(msg proto.Message) RequestModifier {
	if msg == nil {
		return RequestModifier{}
	}

	protoBytes, err := proto.Marshal(msg)
	if err != nil {
		return RequestModifier{
			Kind: core.ModCustom,
			Fn: func(req Request) {
				getOrInitRequestConfig(req).BodyError = err
			},
		}
	}

	frame := make([]byte, 5+len(protoBytes))
	frame[0] = 0x00
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(protoBytes))) //nolint:gosec
	copy(frame[5:], protoBytes)

	capturedFrame := frame

	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			req.SetBodyBytes(capturedFrame)
			req.SetHeader(fheader.ContentType, fheader.MIMEApplicationGRPCWebProto)
			req.SetHeader("X-Grpc-Web", "1")
		},
	}
}

// WithFormValues constructs an [RequestModifier] encoding [url.Values] into the request body as application/x-www-form-urlencoded.
func WithFormValues(values url.Values) RequestModifier {
	encoded := values.Encode()

	return RequestModifier{
		Kind:        core.ModBodyBytes,
		ContentType: fheader.MIMEApplicationForm,
		Bytes:       bytesconv.S2B(encoded),
	}
}

// WithFormBody constructs an [RequestModifier] encoding a struct or map into URL-encoded form values.
func WithFormBody(payload any) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			if payload == nil {
				return
			}

			if r, ok := payload.(io.Reader); ok {
				req.SetBodyStream(r, -1)
				req.SetHeader(fheader.ContentType, fheader.MIMEApplicationForm)
				return
			}

			encoder := values.Encode
			if cfg := pipeline.GetRequestConfig(req.Context()); cfg != nil && cfg.QueryEncoder != nil {
				encoder = cfg.QueryEncoder
			}

			vals, err := encoder(payload)
			if err != nil {
				getOrInitRequestConfig(req).BodyError = err
				return
			}

			encoded := vals.Encode()
			req.SetBodyBytes(bytesconv.S2B(encoded))
			req.SetHeader(fheader.ContentType, fheader.MIMEApplicationForm)
		},
	}
}
