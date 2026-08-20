// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package resiliency

import (
	"slices"
	"time"

	"github.com/lemon4ksan/foundation/sync/backoff"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/middleware"
)

// RetryBuilder constructs a declarative, high-performance request retry and backoff policy.
type RetryBuilder struct {
	maxAttempts        uint32
	backoffStrategy    backoff.Strategy
	honorRetryAfter    bool
	maxRetryAfter      time.Duration
	autoIdempotencyKey bool
	allowedMethods     []string
	conditions         []core.RetryCondition
	onRetry            func(attempt uint32, err error, delay time.Duration)
}

// NewRetry instantiates a clean [RetryBuilder] with 3 default attempts and exponential backoff.
func NewRetry() *RetryBuilder {
	return &RetryBuilder{
		maxAttempts:     3,
		backoffStrategy: backoff.NewExponential(100*time.Millisecond, 5*time.Second, 2.0).WithFullJitter(),
		honorRetryAfter: true,
		maxRetryAfter:   60 * time.Second,
	}
}

// MaxAttempts sets the maximum total attempts (initial request + retries).
func (b *RetryBuilder) MaxAttempts(n uint32) *RetryBuilder {
	if n < 1 {
		n = 1
	}

	b.maxAttempts = n

	return b
}

// ExponentialBackoff configures exponential backoff intervals with 2.0 growth factor.
func (b *RetryBuilder) ExponentialBackoff(initial, max time.Duration) *RetryBuilder {
	b.backoffStrategy = backoff.NewExponential(initial, max, 2.0)
	return b
}

// LinearBackoff configures linearly increasing backoff intervals.
func (b *RetryBuilder) LinearBackoff(initial, max, step time.Duration) *RetryBuilder {
	b.backoffStrategy = backoff.NewLinear(initial, max, step)
	return b
}

// ConstantBackoff configures a static, unchanging sleep interval between attempts.
func (b *RetryBuilder) ConstantBackoff(delay time.Duration) *RetryBuilder {
	b.backoffStrategy = backoff.NewConstant(delay)
	return b
}

// WithFullJitter enables Full Jitter on the current backoff strategy if supported.
func (b *RetryBuilder) WithFullJitter() *RetryBuilder {
	switch s := b.backoffStrategy.(type) {
	case *backoff.ExponentialBackoff:
		s.WithFullJitter()
	case *backoff.LinearBackoff:
		s.WithFullJitter()
	case *backoff.ConstantBackoff:
		s.WithFullJitter()
	}

	return b
}

// WithEqualJitter enables Equal Jitter on the current exponential backoff strategy.
func (b *RetryBuilder) WithEqualJitter() *RetryBuilder {
	if eb, ok := b.backoffStrategy.(*backoff.ExponentialBackoff); ok {
		eb.WithEqualJitter()
	}

	return b
}

// WithDecorrelatedJitter enables Decorrelated Jitter on exponential backoff.
func (b *RetryBuilder) WithDecorrelatedJitter() *RetryBuilder {
	if eb, ok := b.backoffStrategy.(*backoff.ExponentialBackoff); ok {
		eb.WithDecorrelatedJitter()
	}

	return b
}

// WithBackoff assigns a custom [backoff.Strategy].
func (b *RetryBuilder) WithBackoff(strategy backoff.Strategy) *RetryBuilder {
	if strategy != nil {
		b.backoffStrategy = strategy
	}

	return b
}

// HonorRetryAfter enables or disables parsing server 'Retry-After' response headers.
func (b *RetryBuilder) HonorRetryAfter(enabled bool, maxLimit ...time.Duration) *RetryBuilder {
	b.honorRetryAfter = enabled
	if len(maxLimit) > 0 && maxLimit[0] > 0 {
		b.maxRetryAfter = maxLimit[0]
	}

	return b
}

// OnStatus adds retry triggers for specific HTTP response status codes (e.g. 429, 502, 503, 504).
func (b *RetryBuilder) OnStatus(statusCodes ...int) *RetryBuilder {
	codes := append([]int(nil), statusCodes...)
	b.conditions = append(b.conditions, func(resp aoni.Response, err error) bool {
		if resp == nil {
			return false
		}

		return slices.Contains(codes, resp.StatusCode())
	})

	return b
}

// OnTransientErrors triggers retries on network timeouts, broken pipes, and 5xx server errors.
func (b *RetryBuilder) OnTransientErrors() *RetryBuilder {
	b.conditions = append(b.conditions, middleware.RetryOnTransientErrors())
	return b
}

// OnRateLimit triggers retries on HTTP 429 Too Many Requests.
func (b *RetryBuilder) OnRateLimit() *RetryBuilder {
	b.conditions = append(b.conditions, middleware.RetryOnRateLimit())
	return b
}

// OnGatewayErrors triggers retries on HTTP 502, 503, and 504.
func (b *RetryBuilder) OnGatewayErrors() *RetryBuilder {
	b.conditions = append(b.conditions, middleware.RetryOnGatewayErrors())
	return b
}

// OnGRPCStatus triggers retries on specific gRPC trailer error status codes.
func (b *RetryBuilder) OnGRPCStatus(codes ...string) *RetryBuilder {
	b.conditions = append(b.conditions, middleware.RetryOnGRPCStatus(codes...))
	return b
}

// OnCondition attaches a custom predicate function evaluating whether a response or error warrants a retry.
func (b *RetryBuilder) OnCondition(cond core.RetryCondition) *RetryBuilder {
	if cond != nil {
		b.conditions = append(b.conditions, cond)
	}

	return b
}

// AllowedMethods restricts retries strictly to the provided HTTP methods (e.g., "GET", "HEAD", "PUT").
func (b *RetryBuilder) AllowedMethods(methods ...string) *RetryBuilder {
	b.allowedMethods = append([]string(nil), methods...)
	return b
}

// AutoIdempotencyKey automatically generates a unique UUIDv7 Idempotency-Key header on retried requests.
func (b *RetryBuilder) AutoIdempotencyKey() *RetryBuilder {
	b.autoIdempotencyKey = true
	return b
}

// OnRetry registers a hook callback invoked before each retry attempt.
func (b *RetryBuilder) OnRetry(fn func(attempt uint32, err error, delay time.Duration)) *RetryBuilder {
	b.onRetry = fn
	return b
}

// Build compiles the builder configuration into an [aoni.Middleware].
func (b *RetryBuilder) Build() aoni.Middleware {
	opts, cond := b.ToOptions()
	return middleware.Retry(opts, cond)
}

// ToOptions exports the configuration as [middleware.RetryOptions] and a consolidated [aoni.RetryCondition].
func (b *RetryBuilder) ToOptions() (middleware.RetryOptions, core.RetryCondition) {
	initialBackoff := 100 * time.Millisecond
	maxBackoff := 30 * time.Second
	factor := 2.0
	jitter := true
	jitterStrat := middleware.JitterFull

	switch s := b.backoffStrategy.(type) {
	case *backoff.ExponentialBackoff:
		initialBackoff = s.Initial
		maxBackoff = s.Max
		factor = s.Factor

		switch s.Jitter {
		case backoff.JitterNone:
			jitter = false
			jitterStrat = middleware.JitterNone
		case backoff.JitterEqual:
			jitter = true
			jitterStrat = middleware.JitterEqual
		default:
			jitter = true
			jitterStrat = middleware.JitterFull
		}

	case *backoff.ConstantBackoff:
		initialBackoff = s.Delay
		maxBackoff = s.Delay
		factor = 1.0
		jitter = (s.Jitter != backoff.JitterNone)
	}

	opts := middleware.RetryOptions{
		MaxAttempts:        b.maxAttempts,
		InitialBackoff:     initialBackoff,
		MaxBackoff:         maxBackoff,
		BackoffFactor:      factor,
		Jitter:             jitter,
		JitterStrategy:     jitterStrat,
		HonorRetryAfter:    b.honorRetryAfter,
		MaxRetryAfter:      b.maxRetryAfter,
		AutoIdempotencyKey: b.autoIdempotencyKey,
		AllowedMethods:     b.allowedMethods,
		OnRetry:            b.onRetry,
	}

	conditions := b.conditions
	if len(conditions) == 0 {
		conditions = []core.RetryCondition{middleware.RetryOnTransientErrors()}
	}

	compositeCondition := func(resp aoni.Response, err error) bool {
		for _, c := range conditions {
			if c(resp, err) {
				return true
			}
		}

		return false
	}

	return opts, compositeCondition
}

// ToOverride converts the configuration into an [core.RetryOverride] for per-request modifiers.
func (b *RetryBuilder) ToOverride() core.RetryOverride {
	opts, cond := b.ToOptions()

	return core.RetryOverride{
		MaxAttempts: int(opts.MaxAttempts),
		Backoff:     opts.InitialBackoff,
		Condition:   cond,
	}
}
