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

	"google.golang.org/protobuf/proto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

// WithBody constructs an [aoni.RequestModifier] replacing the request body with the provided stream reader.
func WithBody(r stdio.Reader) aoni.RequestModifier {
	return func(req aoni.Request) {
		if r == nil {
			return
		}

		if b, ok := r.(*bytes.Buffer); ok {
			req.SetBodyBytes(b.Bytes())
			return
		}

		if b, ok := r.(*bytes.Reader); ok {
			buf := make([]byte, b.Len())
			_, _ = b.ReadAt(buf, 0)
			req.SetBodyBytes(buf)

			return
		}

		var lenVal int64 = -1
		if b, ok := r.(interface{ Len() int }); ok {
			lenVal = int64(b.Len())
		} else if s, ok := r.(interface{ Len() int64 }); ok {
			lenVal = s.Len()
		}

		req.SetBodyStream(r, lenVal)
	}
}

// WithBodyBytes constructs an [aoni.RequestModifier] setting raw byte slice payload directly as the request body.
func WithBodyBytes(b []byte) aoni.RequestModifier {
	return func(req aoni.Request) {
		req.SetBodyBytes(b)
	}
}

// WithJSONBody constructs an [aoni.RequestModifier] marshaling payload to JSON and setting Content-Type to application/json.
func WithJSONBody(payload any) aoni.RequestModifier {
	return func(req aoni.Request) {
		if payload == nil {
			return
		}

		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			aoni.GetOrInitRequestConfig(req).BodyError = err
			return
		}

		req.SetBodyBytes(bodyBytes)
		req.SetHeader("Content-Type", "application/json")
	}
}

// WithProtoBody constructs an [aoni.RequestModifier] serializing a [proto.Message] into binary Protocol Buffer bytes.
func WithProtoBody(msg proto.Message) aoni.RequestModifier {
	return func(req aoni.Request) {
		if msg == nil {
			return
		}

		bodyBytes, err := proto.Marshal(msg)
		if err != nil {
			aoni.GetOrInitRequestConfig(req).BodyError = err
			return
		}

		req.SetBodyBytes(bodyBytes)
		req.SetHeader("Content-Type", "application/x-protobuf")
		req.SetHeader("Accept", "application/x-protobuf")
	}
}

// WithGRPCWebBody constructs an [aoni.RequestModifier] serializing a [proto.Message] into 5-byte gRPC-Web framed bytes.
func WithGRPCWebBody(msg proto.Message) aoni.RequestModifier {
	return func(req aoni.Request) {
		if msg == nil {
			return
		}

		protoBytes, err := proto.Marshal(msg)
		if err != nil {
			aoni.GetOrInitRequestConfig(req).BodyError = err
			return
		}

		frame := make([]byte, 5+len(protoBytes))
		frame[0] = 0x00
		binary.BigEndian.PutUint32(frame[1:5], uint32(len(protoBytes))) //nolint:gosec
		copy(frame[5:], protoBytes)

		req.SetBodyBytes(frame)
		req.SetHeader("Content-Type", "application/grpc-web+proto")
		req.SetHeader("Accept", "application/grpc-web+proto")
		req.SetHeader("X-Grpc-Web", "1")
		req.SetHeader("X-User-Agent", "grpc-web-javascript/0.1")
	}
}

// WithFormValues constructs an [aoni.RequestModifier] encoding [url.Values] into the request body as application/x-www-form-urlencoded.
func WithFormValues(values url.Values) aoni.RequestModifier {
	return func(req aoni.Request) {
		encoded := values.Encode()
		req.SetBodyBytes(bytesconv.S2B(encoded))
		req.SetHeader("Content-Type", "application/x-www-form-urlencoded")
	}
}

// WithFormBody constructs an [aoni.RequestModifier] encoding a struct or map into URL-encoded form values.
func WithFormBody(payload any) aoni.RequestModifier {
	return func(req aoni.Request) {
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
	}
}
