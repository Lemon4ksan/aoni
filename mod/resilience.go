// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"net/http"
	"time"

	"github.com/lemon4ksan/aoni"
)

// WithRetryPolicy constructs an [aoni.RequestModifier] assigning custom retry parameters to the request.
func WithRetryPolicy(override aoni.RetryOverride) aoni.RequestModifier {
	policy := override
	if policy.MaxAttempts < 1 {
		policy.MaxAttempts = 1
	}

	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).RetryPolicy = &policy
	})
}

// WithAllowNonReadOnlyHedging constructs an [aoni.RequestModifier] permitting request hedging for non-idempotent HTTP methods.
func WithAllowNonReadOnlyHedging(allow bool) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).AllowNonReadOnlyHedging = allow
	})
}

// WithFallback constructs an [aoni.RequestModifier] registering an alternative response fallback generator.
func WithFallback(f aoni.FallbackFunc) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Fallback = f
	})
}

// WithResponseValidator constructs an [aoni.RequestModifier] attaching custom response validation predicates.
func WithResponseValidator(fn func(resp *http.Response) error) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)

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

// WithMultiReadThreshold constructs an [aoni.RequestModifier] configuring RAM buffering bounds for replayable reads.
func WithMultiReadThreshold(threshold int64) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).MultiReadThreshold = threshold
	})
}

// WithMultiReadDisableDisk constructs an [aoni.RequestModifier] disabling temporary file disk backing on buffer overflows.
func WithMultiReadDisableDisk(disable bool) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).MultiReadDisableDisk = disable
	})
}

// WithCacheTTL constructs an [aoni.RequestModifier] configuring custom response caching TTL for the request.
func WithCacheTTL(ttl time.Duration) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).CacheTTL = ttl
	})
}

// WithCoalesce constructs an [aoni.RequestModifier] enabling Singleflight Request Coalescing
// for concurrent identical in-flight operations to prevent upstream thundering herd load.
func WithCoalesce() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).Coalesce = true
	})
}

// WithETag constructs an [aoni.RequestModifier] enabling automated RFC 9111 conditional caching
// and 304 Not Modified body reconstruction.
func WithETag() aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ETagAutomaton = true
	})
}
