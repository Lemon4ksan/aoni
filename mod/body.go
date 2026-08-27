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

// WithBody replaces the request body with the provided [io.Reader] stream.
//
// Automatically detects and optimizes in-memory buffers ([bytes.Buffer], [bytes.Reader], [strings.Reader])
// to avoid heap allocations.
//
// # Example
//
//	file, _ := os.Open("payload.bin")
//	defer file.Close()
//
//	resp, err := client.Post(ctx, "/upload",
//	    mod.WithBody(file),
//	    mod.WithContentType("application/octet-stream"),
//	)
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

// WithBodyBytes sets a raw byte slice directly as the request payload.
//
// Operates on the zero-allocation fast path without buffer copying.
//
// # Example
//
//	resp, err := client.Post(ctx, "/raw",
//	    mod.WithBodyBytes([]byte("hello world")),
//	    mod.WithContentType("text/plain"),
//	)
func WithBodyBytes(b []byte) RequestModifier {
	return RequestModifier{
		Kind:  core.ModBodyBytes,
		Bytes: b,
	}
}

// WithJSONBody marshals payload into JSON, sets Content-Type to "application/json", and attaches it as body.
//
// # Example
//
//	type CreateUserReq struct {
//	    Name  string `json:"name"`
//	    Email string `json:"email"`
//	}
//
//	resp, err := client.Post(ctx, "/users",
//	    mod.WithJSONBody(CreateUserReq{Name: "Alice", Email: "alice@example.com"}),
//	)
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

// WithSmartBody automatically infers the correct encoding and Content-Type based on payload type:
//   - [proto.Message]: Protobuf binary encoding ("application/x-protobuf")
//   - [url.Values]: Form URL-encoding ("application/x-www-form-urlencoded")
//   - [io.Reader]: Streamed payload
//   - `[]byte`: Raw byte payload
//   - `string`: UTF-8 text ("text/plain; charset=utf-8")
//   - Struct / Map / Slice: JSON marshaling ("application/json")
//
// # Example
//
//	// Automatically encoded as JSON with application/json
//	resp, err := client.Post(ctx, "/items",
//	    mod.WithSmartBody(map[string]int{"count": 42}),
//	)
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

// WithXMLBody marshals payload to XML and sets Content-Type to "application/xml".
//
// # Example
//
//	resp, err := client.Post(ctx, "/soap-endpoint",
//	    mod.WithXMLBody(myXMLStruct),
//	)
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

// WithYAMLBody marshals payload to YAML and sets Content-Type to "application/yaml".
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

// WithProtoBody serializes a [proto.Message] into binary Protocol Buffer bytes ("application/x-protobuf").
//
// # Example
//
//	resp, err := client.Post(ctx, "/v1/rpc",
//	    mod.WithProtoBody(protoReq),
//	)
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

// WithGRPCWebBody serializes a [proto.Message] with the standard 5-byte gRPC-Web framing header (1-byte flag + 4-byte length).
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

// WithFormValues encodes [url.Values] into the request body with "application/x-www-form-urlencoded".
//
// # Example
//
//	form := url.Values{}
//	form.Set("grant_type", "client_credentials")
//	form.Set("client_id", "my_id")
//
//	resp, err := client.Post(ctx, "/oauth/token",
//	    mod.WithFormValues(form),
//	)
func WithFormValues(values url.Values) RequestModifier {
	encoded := values.Encode()

	return RequestModifier{
		Kind:        core.ModBodyBytes,
		ContentType: fheader.MIMEApplicationForm,
		Bytes:       bytesconv.S2B(encoded),
	}
}

// WithFormBody serializes a struct or map into URL-encoded form values.
//
// # Example
//
//	type LoginForm struct {
//	    User string `form:"user"`
//	    Pass string `form:"pass"`
//	}
//
//	resp, err := client.Post(ctx, "/login",
//	    mod.WithFormBody(LoginForm{User: "john", Pass: "secret"}),
//	)
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
