// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package resiliency

import (
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/resiliency/cache"
	"github.com/lemon4ksan/aoni/resiliency/challenge"
	"github.com/lemon4ksan/aoni/resiliency/coalesce"
	"github.com/lemon4ksan/aoni/resiliency/etag"
)

type (
	// RetryCondition evaluates whether a failed transaction attempt should trigger a retry.
	RetryCondition = core.RetryCondition

	// RetryOverride overrides default client retry behavior for a specific request execution.
	RetryOverride = core.RetryOverride

	// FallbackFunc generates a synthetic fallback [aoni.Response] when a request execution permanently fails.
	FallbackFunc = core.FallbackFunc

	// CacheStore defines the persistence contract for HTTP response caching backends (e.g. Memory, Redis).
	CacheStore[K comparable, V any] = cache.Store[K, V]

	// ChallengeSolver delegates WAF/DDoS challenge page resolution (e.g. Cloudflare JS/Captcha)
	// to automated headless or external solver drivers.
	ChallengeSolver = challenge.Solver

	// ChallengeDetector determines whether an incoming HTTP response represents a WAF/DDoS challenge page.
	ChallengeDetector = challenge.Detector

	// CircuitBreaker manages host-isolated circuit breakers using thread-safe key locks.
	CircuitBreaker = middleware.CircuitBreaker

	// CircuitBreakerConfig configures failure thresholds, reset timeouts, and key locking for host circuit breakers.
	CircuitBreakerConfig = middleware.CircuitBreakerConfig

	// CoalesceGroup manages concurrent in-flight request deduplication.
	CoalesceGroup = coalesce.Group

	// ETagAutomaton manages RFC 9111 ETag recording and 304 Not Modified reconstruction.
	ETagAutomaton = etag.Automaton
)

var (
	// NewCircuitBreaker initializes a new host-isolated circuit breaker registry.
	NewCircuitBreaker = middleware.NewCircuitBreaker

	// ErrCircuitOpen is returned when a circuit breaker blocks requests to an unhealthy host.
	ErrCircuitOpen = middleware.ErrCircuitOpen

	// DefaultCircuitBreakerCondition flags 5xx server errors or connection failures as circuit breaker triggers.
	DefaultCircuitBreakerCondition = middleware.DefaultCircuitBreakerCondition

	// NewCoalesceGroup creates a new request deduplication [CoalesceGroup].
	NewCoalesceGroup = coalesce.NewGroup

	// NewETagAutomaton creates a new RFC 9111 ETag automaton instance.
	NewETagAutomaton = etag.NewAutomaton
)
