// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc

import (
	"context"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/mod"
)

// metadataContextKey is a private context key for carrying gRPC metadata.
type metadataContextKey struct{}

// WithMetadata produces an [core.RequestModifier] that injects gRPC metadata headers.
// Keys ending in "-bin" are automatically encoded using Base64 per PROTOCOL-HTTP2.md.
func WithMetadata(md Metadata) core.RequestModifier {
	return mod.Custom(func(req core.Request) {
		for k, v := range md {
			if strings.HasSuffix(strings.ToLower(k), "-bin") {
				req.SetHeader(k, EncodeBinaryHeader(bytesconv.S2B(v)))
			} else {
				req.SetHeader(k, v)
			}
		}
	})
}

// WithBinaryHeader produces a [core.RequestModifier] that encodes raw binary bytes as a Base64 gRPC header.
func WithBinaryHeader(key string, val []byte) core.RequestModifier {
	if !strings.HasSuffix(strings.ToLower(key), "-bin") {
		key += "-bin"
	}

	encoded := EncodeBinaryHeader(val)

	return mod.WithHeader(key, encoded)
}

// WithTimeout produces a [core.RequestModifier] setting the gRPC-Timeout header.
func WithTimeout(d time.Duration) core.RequestModifier {
	return mod.WithHeader(header.GRPCTimeout, formatTimeout(d))
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
