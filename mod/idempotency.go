// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"time"

	"github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/randkit"

	"github.com/lemon4ksan/aoni/internal/core"
)

// HeaderIdempotencyKey is the standard HTTP header for idempotency control (IETF Draft).
const HeaderIdempotencyKey = "Idempotency-Key"

// HeaderRequestID is the standard distributed tracing request identifier header.
const HeaderRequestID = header.XRequestID

// WithIdempotencyKey generates and attaches a unique, time-ordered UUIDv7 into the "Idempotency-Key" header (RFC 9562).
//
// Operates on stack memory with absolute zero heap allocations.
// If the header is already populated, it is preserved without modification.
//
// # Wire Representation
//
//	Idempotency-Key: 018e69e4-7d5a-7140-9e6b-123456789abc
//
// # Example
//
//	resp, err := client.Post(ctx, "/payments/charge",
//	    mod.WithIdempotencyKey(),
//	    mod.WithJSONBody(chargePayload),
//	)
//
// # RFC Compliance
//
// Conforms to RFC 9562 (Universally Unique IDentifiers: UUIDv7).
func WithIdempotencyKey() RequestModifier {
	return WithIdempotencyKeyHeader(HeaderIdempotencyKey)
}

// WithIdempotencyKeyHeader injects a unique time-ordered UUIDv7 into the specified custom header name if absent.
func WithIdempotencyKeyHeader(headerName string) RequestModifier {
	if headerName == "" {
		headerName = HeaderIdempotencyKey
	}

	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			if req.Header(headerName) == "" {
				var buf [36]byte

				keyBytes := randkit.AppendUUIDv7(buf[:0], time.Now())
				req.SetHeader(headerName, bytesconv.B2S(keyBytes))
			}
		},
	}
}

// WithRequestID generates and attaches a unique time-ordered UUIDv7 into the "X-Request-ID" header for distributed tracing.
//
// # Example
//
//	resp, err := client.Get(ctx, "/status",
//	    mod.WithRequestID(),
//	)
func WithRequestID() RequestModifier {
	return WithRequestIDHeader(HeaderRequestID)
}

// WithRequestIDHeader injects a unique time-ordered UUIDv7 into the specified header name for correlation tracking.
func WithRequestIDHeader(headerName string) RequestModifier {
	if headerName == "" {
		headerName = HeaderRequestID
	}

	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			if req.Header(headerName) == "" {
				var buf [36]byte

				keyBytes := randkit.AppendUUIDv7(buf[:0], time.Now())
				req.SetHeader(headerName, bytesconv.B2S(keyBytes))
			}
		},
	}
}
