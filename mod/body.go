// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	stdio "io"
	"net/url"
	"strings"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/values"
)

// WithBody constructs an [aoni.RequestModifier] replacing the request body with the provided stream reader.
func WithBody(r stdio.Reader) aoni.RequestModifier {
	if r == nil {
		return aoni.RequestModifier{}
	}

	if b, ok := r.(*bytes.Buffer); ok {
		return aoni.RequestModifier{
			Kind:  aoni.ModBodyBytes,
			Bytes: b.Bytes(),
		}
	}

	if br, ok := r.(*bytes.Reader); ok {
		buf := make([]byte, br.Len())
		_, _ = br.ReadAt(buf, 0)

		return aoni.RequestModifier{
			Kind:  aoni.ModBodyBytes,
			Bytes: buf,
		}
	}

	if sr, ok := r.(*strings.Reader); ok {
		buf := make([]byte, sr.Len())
		_, _ = sr.ReadAt(buf, 0)

		return aoni.RequestModifier{
			Kind:  aoni.ModBodyBytes,
			Bytes: buf,
		}
	}

	return aoni.RequestModifier{
		Kind:   aoni.ModBodyStream,
		Stream: r,
	}
}

// WithBodyBytes constructs an [aoni.RequestModifier] setting raw byte slice payload directly as the request body.
func WithBodyBytes(b []byte) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind:  aoni.ModBodyBytes,
		Bytes: b,
	}
}

// WithJSONBody constructs an [aoni.RequestModifier] marshaling payload to JSON and setting Content-Type to application/json.
func WithJSONBody(payload any) aoni.RequestModifier {
	if payload == nil {
		return aoni.RequestModifier{}
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return aoni.RequestModifier{
			Kind: aoni.ModCustom,
			Fn: func(req aoni.Request) {
				aoni.GetOrInitRequestConfig(req).BodyError = err
			},
		}
	}

	return aoni.RequestModifier{
		Kind:        aoni.ModBodyBytes,
		ContentType: "application/json",
		Bytes:       bodyBytes,
	}
}

// WithProtoBody constructs an [aoni.RequestModifier] serializing a [proto.Message] into binary Protocol Buffer bytes.
func WithProtoBody(msg proto.Message) aoni.RequestModifier {
	if msg == nil {
		return aoni.RequestModifier{}
	}

	bodyBytes, err := proto.Marshal(msg)
	if err != nil {
		return aoni.RequestModifier{
			Kind: aoni.ModCustom,
			Fn: func(req aoni.Request) {
				aoni.GetOrInitRequestConfig(req).BodyError = err
			},
		}
	}

	return aoni.RequestModifier{
		Kind:        aoni.ModBodyBytes,
		ContentType: "application/x-protobuf",
		Bytes:       bodyBytes,
	}
}

// WithGRPCWebBody constructs an [aoni.RequestModifier] serializing a [proto.Message] into 5-byte gRPC-Web framed bytes.
func WithGRPCWebBody(msg proto.Message) aoni.RequestModifier {
	if msg == nil {
		return aoni.RequestModifier{}
	}

	protoBytes, err := proto.Marshal(msg)
	if err != nil {
		return aoni.RequestModifier{
			Kind: aoni.ModCustom,
			Fn: func(req aoni.Request) {
				aoni.GetOrInitRequestConfig(req).BodyError = err
			},
		}
	}

	frame := make([]byte, 5+len(protoBytes))
	frame[0] = 0x00
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(protoBytes))) //nolint:gosec
	copy(frame[5:], protoBytes)

	capturedFrame := frame

	return aoni.RequestModifier{
		Kind: aoni.ModCustom,
		Fn: func(req aoni.Request) {
			req.SetBodyBytes(capturedFrame)
			req.SetHeader("Content-Type", "application/grpc-web+proto")
			req.SetHeader("X-Grpc-Web", "1")
		},
	}
}

// WithFormValues constructs an [aoni.RequestModifier] encoding [url.Values] into the request body as application/x-www-form-urlencoded.
func WithFormValues(values url.Values) aoni.RequestModifier {
	encoded := values.Encode()

	return aoni.RequestModifier{
		Kind:        aoni.ModBodyBytes,
		ContentType: "application/x-www-form-urlencoded",
		Bytes:       bytesconv.S2B(encoded),
	}
}

// WithFormBody constructs an [aoni.RequestModifier] encoding a struct or map into URL-encoded form values.
func WithFormBody(payload any) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind: aoni.ModCustom,
		Fn: func(req aoni.Request) {
			if payload == nil {
				return
			}

			if r, ok := payload.(stdio.Reader); ok {
				req.SetBodyStream(r, -1)
				req.SetHeader("Content-Type", "application/x-www-form-urlencoded")
				return
			}

			encoder := values.StructToValues
			if cfg := aoni.GetRequestConfig(req.Context()); cfg != nil && cfg.QueryEncoder != nil {
				encoder = cfg.QueryEncoder
			}

			vals, err := encoder(payload)
			if err != nil {
				aoni.GetOrInitRequestConfig(req).BodyError = err
				return
			}

			encoded := vals.Encode()
			req.SetBodyBytes(bytesconv.S2B(encoded))
			req.SetHeader("Content-Type", "application/x-www-form-urlencoded")
		},
	}
}
