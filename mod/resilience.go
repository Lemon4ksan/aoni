// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"net/http"
	"time"

	"github.com/lemon4ksan/aoni/internal/core"
)

// WithRetryPolicy assigns a granular [core.RetryOverride] retry and backoff policy to this specific request.
//
// # Example
//
//	resp, err := client.Get(ctx, "/flakey-api",
//	    mod.WithRetryPolicy(core.RetryOverride{
//	        MaxAttempts: 5,
//	    }),
//	)
func WithRetryPolicy(override core.RetryOverride) RequestModifier {
	policy := override
	if policy.MaxAttempts < 1 {
		policy.MaxAttempts = 1
	}

	return Custom(func(req Request) {
		getOrInitRequestConfig(req).RetryPolicy = &policy
	})
}

// WithRetry sets the maximum number of retry attempts for this request.
//
// # Example
//
//	resp, err := client.Get(ctx, "/endpoint",
//	    mod.WithRetry(3),
//	)
func WithRetry(attempts int) RequestModifier {
	return WithRetryPolicy(core.RetryOverride{MaxAttempts: attempts})
}

// WithAllowNonReadOnlyHedging permits speculative request hedging for mutating HTTP methods (POST, PUT, DELETE).
//
// > [!WARNING]
// > Hedging non-idempotent operations can trigger duplicate state transitions upstream if the server lacks idempotency controls.
func WithAllowNonReadOnlyHedging(allow bool) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).AllowNonReadOnlyHedging = allow
	})
}

// WithFallback registers a fallback generator function called to provide a synthetic response when remote execution fails.
func WithFallback(f core.FallbackFunc) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Fallback = f
	})
}

// WithResponseValidator attaches a per-request response validator executed before decoding.
func WithResponseValidator(fn func(resp *http.Response) error) RequestModifier {
	return Custom(func(req Request) {
		cfg := getOrInitRequestConfig(req)

		existing := cfg.ResponseValidator
		if existing != nil {
			cfg.ResponseValidator = func(resp *http.Response) error {
				if err := existing(resp); err != nil {
					return err
				}

				return fn(resp)
			}

			return
		}

		cfg.ResponseValidator = fn
	})
}

// WithSoftErrorDetector attaches callbacks that sniff response body prefixes to detect application-level errors
// without draining or consuming the response stream.
//
//nolint:bodyclose // Soft error detectors inspect responses without taking ownership of response lifecycle.
func WithSoftErrorDetector(detectors ...core.SoftErrorDetector) RequestModifier {
	return Custom(func(req Request) {
		cfg := getOrInitRequestConfig(req)
		for _, det := range detectors {
			if det != nil {
				cfg.SoftErrorDetectors = append(cfg.SoftErrorDetectors, det)
			}
		}
	})
}

// WithMultiReadThreshold sets the memory buffer threshold in bytes for replayable body reads.
func WithMultiReadThreshold(threshold int64) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).MultiReadThreshold = threshold
	})
}

// WithMultiReadDisableDisk disables temporary file disk backing on multi-read buffer overflows.
func WithMultiReadDisableDisk(disable bool) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).MultiReadDisableDisk = disable
	})
}

// WithCacheTTL overrides the cache expiration TTL for this specific request.
//
// # Example
//
//	resp, err := client.Get(ctx, "/slow-report",
//	    mod.WithCacheTTL(10 * time.Minute),
//	)
func WithCacheTTL(ttl time.Duration) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).CacheTTL = ttl
	})
}

// WithCoalesce enables Singleflight Request Coalescing for this request.
//
// Multiple concurrent goroutines dispatching identical requests will share a single in-flight network round-trip,
// eliminating upstream thundering herd effects.
//
// # Example
//
//	resp, err := client.Get(ctx, "/hot-key",
//	    mod.WithCoalesce(),
//	)
func WithCoalesce() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Coalesce = true
	})
}

// WithETag enables automated RFC 9111 conditional caching and 304 Not Modified body reconstruction for this request.
//
// # RFC Compliance
//
// Conforms to RFC 9111 (HTTP Caching) Section 4.3.
func WithETag() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ETagAutomaton = true
	})
}
