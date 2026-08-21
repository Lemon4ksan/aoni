// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package middleware

import (
	"errors"
	"time"

	"github.com/lemon4ksan/foundation/async/rate"
	"github.com/lemon4ksan/foundation/sync/limiter"

	"github.com/lemon4ksan/aoni"
)

// ErrSlidingWindowCanceled indicates that context was canceled while waiting for a rate limiter slot.
var ErrSlidingWindowCanceled = errors.New("aoni: sliding window rate limit wait canceled")

// RateLimit constructs an [aoni.Middleware] enforcing token-bucket rate limits per second with burst capacity.
func RateLimit(requestsPerSecond float64, burst int) aoni.Middleware {
	limiter := rate.NewLimiter(rate.Limit(requestsPerSecond), burst)

	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			if err := limiter.Wait(req.Context()); err != nil {
				return nil, err
			}

			return next.Do(req)
		})
	}
}

// SlidingWindowLimiter implements a thread-safe sliding window log rate limiter.
type SlidingWindowLimiter = limiter.SlidingWindowLimiter

// NewSlidingWindowLimiter constructs a new sliding window rate limiter instance.
func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	return limiter.NewSlidingWindowLimiter(limit, window)
}

// LimitEnforcer constructs an [aoni.Middleware] wrapping a [SlidingWindowLimiter].
func LimitEnforcer(limiter *SlidingWindowLimiter) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			ctx := req.Context()

			for {
				allowed, waitTime := limiter.Allow(time.Now())
				if allowed {
					return next.Do(req)
				}

				timer := time.NewTimer(waitTime)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, ErrSlidingWindowCanceled
				case <-timer.C:
				}
			}
		})
	}
}

// AdaptiveLimit constructs an [aoni.Middleware] wrapping a [limiter.AdaptiveLimiter] concurrency controller.
func AdaptiveLimit(limiter *limiter.AdaptiveLimiter) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			ctx := req.Context()
			if err := limiter.Acquire(ctx); err != nil {
				return nil, err
			}

			startTime := time.Now()
			resp, err := next.Do(req)
			duration := time.Since(startTime)

			limiter.Release(duration)

			return resp, err
		})
	}
}
