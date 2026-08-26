// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"net/http"
	"time"

	"github.com/lemon4ksan/aoni/internal/core"
)

// WithRetryPolicy constructs an [RequestModifier] assigning custom retry parameters to the request.
func WithRetryPolicy(override core.RetryOverride) RequestModifier {
	policy := override
	if policy.MaxAttempts < 1 {
		policy.MaxAttempts = 1
	}

	return Custom(func(req Request) {
		getOrInitRequestConfig(req).RetryPolicy = &policy
	})
}

// WithRetry constructs an [RequestModifier] that sets the maximum retry attempts for the request.
func WithRetry(attempts int) RequestModifier {
	return WithRetryPolicy(core.RetryOverride{MaxAttempts: attempts})
}

// WithAllowNonReadOnlyHedging constructs an [RequestModifier] permitting request hedging for non-idempotent HTTP methods.
func WithAllowNonReadOnlyHedging(allow bool) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).AllowNonReadOnlyHedging = allow
	})
}

// WithFallback constructs an [RequestModifier] registering an alternative response fallback generator.
func WithFallback(f core.FallbackFunc) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Fallback = f
	})
}

// WithResponseValidator constructs an [RequestModifier] attaching custom response validation predicates.
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

// WithSoftErrorDetector constructs an [RequestModifier] attaching soft error detection callbacks.
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

// WithMultiReadThreshold constructs an [RequestModifier] configuring RAM buffering bounds for replayable reads.
func WithMultiReadThreshold(threshold int64) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).MultiReadThreshold = threshold
	})
}

// WithMultiReadDisableDisk constructs an [RequestModifier] disabling temporary file disk backing on buffer overflows.
func WithMultiReadDisableDisk(disable bool) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).MultiReadDisableDisk = disable
	})
}

// WithCacheTTL constructs an [RequestModifier] configuring custom response caching TTL for the request.
func WithCacheTTL(ttl time.Duration) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).CacheTTL = ttl
	})
}

// WithCoalesce constructs an [RequestModifier] enabling Singleflight Request Coalescing
// for concurrent identical in-flight operations to prevent upstream thundering herd load.
func WithCoalesce() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Coalesce = true
	})
}

// WithETag constructs an [RequestModifier] enabling automated RFC 9111 conditional caching
// and 304 Not Modified body reconstruction.
func WithETag() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ETagAutomaton = true
	})
}
