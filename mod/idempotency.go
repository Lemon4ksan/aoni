// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"time"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/rand"

	"github.com/lemon4ksan/aoni"
)

// HeaderIdempotencyKey is the standard HTTP header for idempotency control (IETF Draft).
const HeaderIdempotencyKey = "Idempotency-Key"

// HeaderRequestID is the standard distributed tracing request identifier header.
const HeaderRequestID = "X-Request-ID"

// WithIdempotencyKey constructs an [aoni.RequestModifier] injecting a unique time-ordered
// UUIDv7 into the "Idempotency-Key" header (RFC 9562) if the header is not already set.
// It executes on stack memory with zero heap allocations.
func WithIdempotencyKey() aoni.RequestModifier {
	return WithIdempotencyKeyHeader(HeaderIdempotencyKey)
}

// WithIdempotencyKeyHeader constructs an [aoni.RequestModifier] injecting a unique time-ordered
// UUIDv7 into the specified header name if the header is not already set.
func WithIdempotencyKeyHeader(headerName string) aoni.RequestModifier {
	if headerName == "" {
		headerName = HeaderIdempotencyKey
	}

	return aoni.RequestModifier{
		Kind: aoni.ModCustom,
		Fn: func(req aoni.Request) {
			if req.Header(headerName) == "" {
				var buf [36]byte

				keyBytes := rand.AppendUUIDv7(buf[:0], time.Now())
				req.SetHeader(headerName, bytesconv.B2S(keyBytes))
			}
		},
	}
}

// WithRequestID constructs an [aoni.RequestModifier] injecting a unique time-ordered
// UUIDv7 into the "X-Request-ID" header if the header is not already set.
func WithRequestID() aoni.RequestModifier {
	return WithRequestIDHeader(HeaderRequestID)
}

// WithRequestIDHeader constructs an [aoni.RequestModifier] injecting a unique time-ordered
// UUIDv7 into the specified header name if the header is not already set.
func WithRequestIDHeader(headerName string) aoni.RequestModifier {
	if headerName == "" {
		headerName = HeaderRequestID
	}

	return aoni.RequestModifier{
		Kind: aoni.ModCustom,
		Fn: func(req aoni.Request) {
			if req.Header(headerName) == "" {
				var buf [36]byte

				keyBytes := rand.AppendUUIDv7(buf[:0], time.Now())
				req.SetHeader(headerName, bytesconv.B2S(keyBytes))
			}
		},
	}
}
