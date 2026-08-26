// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"time"

	fheader "github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/rand"

	"github.com/lemon4ksan/aoni/internal/core"
)

// HeaderIdempotencyKey is the standard HTTP header for idempotency control (IETF Draft).
const HeaderIdempotencyKey = "Idempotency-Key"

// HeaderRequestID is the standard distributed tracing request identifier header.
const HeaderRequestID = fheader.XRequestID

// WithIdempotencyKey constructs an [RequestModifier] injecting a unique time-ordered
// UUIDv7 into the "Idempotency-Key" header (RFC 9562) if the header is not already set.
// It executes on stack memory with zero heap allocations.
func WithIdempotencyKey() RequestModifier {
	return WithIdempotencyKeyHeader(HeaderIdempotencyKey)
}

// WithIdempotencyKeyHeader constructs an [RequestModifier] injecting a unique time-ordered
// UUIDv7 into the specified header name if the header is not already set.
func WithIdempotencyKeyHeader(headerName string) RequestModifier {
	if headerName == "" {
		headerName = HeaderIdempotencyKey
	}

	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			if req.Header(headerName) == "" {
				var buf [36]byte

				keyBytes := rand.AppendUUIDv7(buf[:0], time.Now())
				req.SetHeader(headerName, bytesconv.B2S(keyBytes))
			}
		},
	}
}

// WithRequestID constructs an [RequestModifier] injecting a unique time-ordered
// UUIDv7 into the "X-Request-ID" header if the header is not already set.
func WithRequestID() RequestModifier {
	return WithRequestIDHeader(HeaderRequestID)
}

// WithRequestIDHeader constructs an [RequestModifier] injecting a unique time-ordered
// UUIDv7 into the specified header name if the header is not already set.
func WithRequestIDHeader(headerName string) RequestModifier {
	if headerName == "" {
		headerName = HeaderRequestID
	}

	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			if req.Header(headerName) == "" {
				var buf [36]byte

				keyBytes := rand.AppendUUIDv7(buf[:0], time.Now())
				req.SetHeader(headerName, bytesconv.B2S(keyBytes))
			}
		},
	}
}
