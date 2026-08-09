// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"context"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
)

type metadataContextKey struct{}

// WithMetadata produces an [aoni.RequestModifier] that injects gRPC metadata headers.
// Keys ending in "-bin" are automatically encoded using Base64 per PROTOCOL-HTTP2.md.
func WithMetadata(md Metadata) aoni.RequestModifier {
	return func(req aoni.Request) {
		for k, v := range md {
			if strings.HasSuffix(strings.ToLower(k), "-bin") {
				req.SetHeader(k, EncodeBinaryHeader([]byte(v)))
			} else {
				req.SetHeader(k, v)
			}
		}
	}
}

// WithBinaryHeader produces an [aoni.RequestModifier] that encodes raw binary bytes as a Base64 gRPC header.
func WithBinaryHeader(key string, val []byte) aoni.RequestModifier {
	if !strings.HasSuffix(strings.ToLower(key), "-bin") {
		key += "-bin"
	}

	encoded := EncodeBinaryHeader(val)

	return mod.WithHeader(key, encoded)
}

// WithTimeout produces an [aoni.RequestModifier] setting the gRPC-Timeout header.
func WithTimeout(d time.Duration) aoni.RequestModifier {
	return mod.WithHeader("grpc-timeout", formatTimeout(d))
}

// NewContext returns a new context carrying gRPC metadata.
func NewContext(ctx context.Context, md Metadata) context.Context {
	return context.WithValue(ctx, metadataContextKey{}, md)
}

// FromContext extracts gRPC metadata stored in context, if present.
func FromContext(ctx context.Context) (Metadata, bool) {
	md, ok := ctx.Value(metadataContextKey{}).(Metadata)

	return md, ok
}
