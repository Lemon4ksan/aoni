// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package middleware provides HTTP middleware functions for request logging, rate limiting, and other common use cases.
package middleware

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/sync/breaker"
	"github.com/lemon4ksan/miyako/sync/keylock"
	"github.com/lemon4ksan/miyako/sync/limiter"
	"golang.org/x/time/rate"

	"github.com/lemon4ksan/aoni"
)

// Chain wraps doer with middlewares from left to right: the first
// middleware in the slice executes first. Returns doer unmodified
// when middlewares is empty.
func Chain(doer aoni.HTTPDoer, middlewares ...aoni.Middleware) aoni.HTTPDoer {
	for i := len(middlewares) - 1; i >= 0; i-- {
		doer = middlewares[i](doer)
	}

	return doer
}

// Log returns a middleware that logs HTTP requests using the provided logger.
//
// It hides the sensitive values for keys "key", "access_token" and "token".
func Log(logger aoni.Logger) aoni.Middleware {
	return func(next aoni.HTTPDoer) aoni.HTTPDoer {
		return aoni.DoerFunc(func(req *http.Request) (*http.Response, error) {
			start := time.Now()
			resp, err := next.Do(req)

			logger.Info("http request",
				"method", req.Method,
				"url", maskQueryParams(req.URL),
				"duration", time.Since(start),
				"error", err,
			)

			return resp, err
		})
	}
}

// RateLimit returns a [Middleware] that blocks when the
// request rate exceeds rps with burst tolerance. The limiter uses
// a token bucket algorithm from [golang.org/x/time/rate].
func RateLimit(rps float64, burst int) aoni.Middleware {
	if rps < 0 {
		rps = 0
	}

	if burst < 0 {
		burst = 0
	}

	limiter := rate.NewLimiter(rate.Limit(rps), burst)

	return func(next aoni.HTTPDoer) aoni.HTTPDoer {
		return aoni.DoerFunc(func(req *http.Request) (*http.Response, error) {
			if err := limiter.Wait(req.Context()); err != nil {
				return nil, fmt.Errorf("aoni: rate limit wait failed: %w", err)
			}

			return next.Do(req)
		})
	}
}

// SlidingWindowLimiter implements an O(1) ring-buffer sliding window rate limiter
// without memory allocations or slice memory copies during timestamp purging.
type SlidingWindowLimiter struct {
	mu         sync.Mutex
	limit      int
	window     time.Duration
	timestamps []time.Time
	head       int
	tail       int
	count      int
}

// NewSlidingWindowLimiter creates a new ring-buffer sliding window rate limiter.
func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	if limit <= 0 {
		limit = 1
	}

	return &SlidingWindowLimiter{
		limit:      limit,
		window:     window,
		timestamps: make([]time.Time, limit),
	}
}

// Allow checks if a request is allowed at time now.
// Returns true and 0 wait time if allowed, or false and the required wait duration if limit is reached.
func (l *SlidingWindowLimiter) Allow(now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-l.window)

	// Purge expired timestamps from head in O(1) without memory copy churn
	for l.count > 0 && !l.timestamps[l.head].After(cutoff) {
		l.head = (l.head + 1) % l.limit
		l.count--
	}

	if l.count < l.limit {
		l.timestamps[l.tail] = now
		l.tail = (l.tail + 1) % l.limit
		l.count++

		return true, 0
	}

	// Calculate wait time until oldest request in rolling window expires
	oldest := l.timestamps[l.head]

	waitTime := oldest.Add(l.window).Sub(now)
	if waitTime < 0 {
		waitTime = 0
	}

	return false, waitTime
}

// SlidingWindowRateLimit returns a [Middleware] that enforces strict sliding window rate limiting.
// Guarantees no more than limit requests are dispatched within any rolling window duration.
func SlidingWindowRateLimit(limit int, window time.Duration) aoni.Middleware {
	limiter := NewSlidingWindowLimiter(limit, window)

	return func(next aoni.HTTPDoer) aoni.HTTPDoer {
		return aoni.DoerFunc(func(req *http.Request) (*http.Response, error) {
			for {
				now := time.Now()

				allowed, waitTime := limiter.Allow(now)
				if allowed {
					break
				}

				timer := time.NewTimer(waitTime)
				select {
				case <-req.Context().Done():
					timer.Stop()
					return nil, fmt.Errorf("aoni: sliding window rate limit wait canceled: %w", req.Context().Err())
				case <-timer.C:
					timer.Stop()
				}
			}

			return next.Do(req)
		})
	}
}

// JitterStrategy selects the algorithm for computing retry delay noise.
type JitterStrategy int

const (
	// JitterEqual adds +/- 10% random noise to the exponential backoff.
	JitterEqual JitterStrategy = iota
	// JitterFull picks a random duration between zero and the backoff value.
	JitterFull
)

// RetryOptions configures [Retry].
type RetryOptions struct {
	// MaxRetries is the maximum number of retry attempts after the initial failure (0 = no retries).
	MaxRetries uint32

	// Backoff is the delay before the first retry. Subsequent retries
	// use exponential backoff starting from this value.
	Backoff time.Duration

	// JitterStrategy selects the noise algorithm applied to each delay.
	JitterStrategy JitterStrategy

	// OnRetry is an optional callback executed before each sleep during retry attempts.
	// Provides the attempt count (1-based), the causing error or nil, and the planned backoff delay.
	OnRetry func(attempt uint32, err error, delay time.Duration)
}

// Retry returns a [Middleware] that retries requests
// matching condition up to opts.MaxRetries times. The request
// body is buffered in memory so it can be replayed. The middleware
// respects the Retry-After header when present and falls back to
// exponential backoff with jitter.
//
// If the request carries a [WithRetryPolicy] override, its values take
// precedence over the global opts and condition for that request only.
func Retry(opts RetryOptions, condition aoni.RetryCondition) aoni.Middleware {
	opts.MaxRetries = generic.Coalesce(opts.MaxRetries, 3)
	opts.Backoff = max(generic.Coalesce(opts.Backoff, 1*time.Second), 0)

	return func(next aoni.HTTPDoer) aoni.HTTPDoer {
		return aoni.DoerFunc(func(req *http.Request) (*http.Response, error) {
			// Per-request override takes precedence over global opts.
			activeOpts := opts

			activeCond := condition
			if override, ok := aoni.GetRetryOverride(req.Context()).Value(); ok {
				if override.MaxAttempts > 0 {
					activeOpts.MaxRetries = uint32(override.MaxAttempts - 1) //nolint:gosec
				}

				if override.Backoff > 0 {
					activeOpts.Backoff = override.Backoff
				}

				if override.Condition != nil {
					activeCond = override.Condition
				}
			}

			var (
				body []byte
				err  error
			)

			if req.Body != nil && req.Body != http.NoBody {
				body, err = io.ReadAll(req.Body)
				if err != nil {
					return nil, fmt.Errorf("aoni: failed to read request body for retry: %w", err)
				}

				_ = req.Body.Close()
			}

			var bo *generic.Backoff
			switch activeOpts.JitterStrategy {
			case JitterFull:
				bo = generic.NewBackoff(activeOpts.Backoff, activeOpts.Backoff*32, 2, 1.0)
			default:
				bo = generic.NewBackoff(activeOpts.Backoff, activeOpts.Backoff*32, 2, 0.5)
			}

			for i := uint32(0); i <= activeOpts.MaxRetries; i++ {
				if body != nil {
					req.Body = io.NopCloser(bytes.NewReader(body))
				}

				resp, err := next.Do(req)

				if i < activeOpts.MaxRetries && activeCond(resp, err) {
					if err != nil && isFatalError(err) {
						return resp, err
					}

					retryAfter, hasRetryAfter := parseRetryAfter(resp)
					if resp != nil {
						_ = resp.Body.Close()
					}

					var sleepTime time.Duration
					if hasRetryAfter {
						sleepTime = retryAfter
					} else {
						sleepTime = bo.Next()
					}

					if activeOpts.OnRetry != nil {
						activeOpts.OnRetry(i+1, err, sleepTime)
					}

					select {
					case <-req.Context().Done():
						return nil, req.Context().Err()
					case <-time.After(sleepTime):
						continue
					}
				}

				return resp, err
			}

			return nil, errors.New("aoni: max retries exceeded")
		})
	}
}

// Recover catches panics during request execution, calls
// onPanic with the recovered value (if non-nil), and returns an error.
func Recover(onPanic func(any)) aoni.Middleware {
	return func(next aoni.HTTPDoer) aoni.HTTPDoer {
		return aoni.DoerFunc(func(req *http.Request) (resp *http.Response, err error) {
			defer func() {
				if r := recover(); r != nil {
					if onPanic != nil {
						onPanic(r)
					}

					err = fmt.Errorf("aoni: panic recovered during request execution: %v", r)
				}
			}()

			return next.Do(req)
		})
	}
}

// CircuitBreakerConfig tunes the thresholds for [CircuitBreaker].
// It wraps [breaker.Config] with a per-host map.
type CircuitBreakerConfig struct {
	// FailureThreshold is the ratio of failures (0.0 to 1.0) that triggers the open state.
	FailureThreshold float64
	// Cooldown is the duration the breaker remains open before transitioning to half-open.
	Cooldown time.Duration
	// MinRequests is the minimum number of requests in a Window before threshold check is active.
	MinRequests int
	// Window is the sliding time duration over which failures are tracked.
	Window time.Duration
}

// CircuitBreaker tracks per-host connection health using a sliding window.
// After the failure ratio within [CircuitBreakerConfig.Window] exceeds
// [CircuitBreakerConfig.FailureThreshold], the circuit opens and rejects requests
// for [CircuitBreakerConfig.Cooldown]. It then enters half-open and allows a
// single trial request; success closes it.
type CircuitBreaker struct {
	cfg      CircuitBreakerConfig
	km       keylock.KeyMutex[string]
	breakers map[string]*breaker.CircuitBreaker[any]
	mu       sync.Mutex
}

// NewCircuitBreaker creates a [CircuitBreaker] with cfg. Zero
// fields default to 50% failure threshold, 10s window, 5 min requests,
// and 5s cooldown.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 || cfg.FailureThreshold > 1.0 || math.IsNaN(cfg.FailureThreshold) {
		cfg.FailureThreshold = 0.5
	}

	cfg.Cooldown = generic.Coalesce(cfg.Cooldown, 5*time.Second)
	cfg.MinRequests = generic.Coalesce(cfg.MinRequests, 5)
	cfg.Window = generic.Coalesce(cfg.Window, 10*time.Second)

	return &CircuitBreaker{
		cfg:      cfg,
		breakers: make(map[string]*breaker.CircuitBreaker[any]),
	}
}

func (cb *CircuitBreaker) getBreaker(host string) *breaker.CircuitBreaker[any] {
	cb.mu.Lock()
	b, ok := cb.breakers[host]
	cb.mu.Unlock()

	if ok {
		return b
	}

	cb.km.Lock(host)
	defer cb.km.Unlock(host)

	cb.mu.Lock()

	b, ok = cb.breakers[host]
	if !ok {
		b = breaker.New[any](breaker.Config{
			FailureThreshold: cb.cfg.FailureThreshold,
			Cooldown:         cb.cfg.Cooldown,
			MinRequests:      cb.cfg.MinRequests,
			Window:           cb.cfg.Window,
		})
		cb.breakers[host] = b
	}

	cb.mu.Unlock()

	return b
}

// DefaultCircuitBreakerCondition returns true for network errors and HTTP status codes >= 500.
func DefaultCircuitBreakerCondition(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}

	if resp != nil {
		return resp.StatusCode >= 500
	}

	return false
}

// CircuitBreak returns a [Middleware] that gates
// requests through cb per host. When the circuit is open the
// request fails immediately with an error. isFailure determines
// which responses count as failures; nil uses
// [DefaultCircuitBreakerCondition].
//
// The circuit breaker uses a sliding window: failures are tracked
// over [CircuitBreakerConfig.Window] and compared against
// [CircuitBreakerConfig.FailureThreshold] ratio.
func CircuitBreak(cb *CircuitBreaker, isFailure func(*http.Response, error) bool) aoni.Middleware {
	if isFailure == nil {
		isFailure = DefaultCircuitBreakerCondition
	}

	return func(next aoni.HTTPDoer) aoni.HTTPDoer {
		return aoni.DoerFunc(func(req *http.Request) (*http.Response, error) {
			host := req.URL.Host
			b := cb.getBreaker(host)

			// Execute through the breaker to check state (open/half-open/closed).
			// We always return the response to the caller, but signal the breaker
			// about success/failure via the error channel.
			var resultResp *http.Response

			_, breakerErr := b.Do(req.Context(), func(ctx context.Context) (any, error) {
				resp, err := next.Do(req.WithContext(ctx))
				if err != nil {
					return nil, err
				}

				resultResp = resp

				body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
				_ = resp.Body.Close()

				if isFailure(resp, nil) {
					return nil, fmt.Errorf("aoni: circuit breaker recorded failure (status %d)", resp.StatusCode)
				}

				resp.Body = io.NopCloser(bytes.NewReader(body))

				return nil, nil
			})

			if errors.Is(breakerErr, breaker.ErrCircuitOpen) {
				return nil, fmt.Errorf("aoni: circuit breaker open for host %s", host)
			}

			// If the breaker recorded a failure (not circuit-open), return
			// the response so the caller can inspect the status code.
			if resultResp != nil {
				return resultResp, nil
			}

			return nil, breakerErr
		})
	}
}

// Fallback returns a [Middleware] that invokes the
// [FallbackFunc] registered via [WithFallback] when the request
// fails with any error.
func Fallback() aoni.Middleware {
	return FallbackEx(nil)
}

// FallbackEx is like [Fallback] but uses isFailure
// to decide which responses trigger the fallback. When isFailure is
// nil, any non-nil error triggers it.
func FallbackEx(isFailure func(*http.Response, error) bool) aoni.Middleware {
	if isFailure == nil {
		isFailure = func(resp *http.Response, err error) bool { return err != nil }
	}

	return func(next aoni.HTTPDoer) aoni.HTTPDoer {
		return aoni.DoerFunc(func(req *http.Request) (*http.Response, error) {
			resp, err := next.Do(req)
			if isFailure(resp, err) {
				cfg := aoni.GetRequestConfig(req.Context())
				if cfg != nil && cfg.Fallback != nil {
					fallbackResp, fallbackErr := cfg.Fallback(req, err)
					if fallbackErr == nil {
						if resp != nil && resp.Body != nil {
							_ = resp.Body.Close()
						}

						return fallbackResp, nil
					}
				}
			}

			return resp, err
		})
	}
}

// ChaosConfig defines parameters for [Chaos].
type ChaosConfig struct {
	// FailureRate is the probability (0.0 to 1.0) of randomly returning a 503 error.
	FailureRate float64
	// LatencyMin is the minimum artificial delay duration applied to requests.
	LatencyMin time.Duration
	// LatencyMax is the maximum artificial delay duration applied to requests.
	LatencyMax time.Duration
}

// Chaos returns a [Middleware] that injects random latency
// and 503 errors. Useful for testing retry and circuit breaker logic.
func Chaos(cfg ChaosConfig) aoni.Middleware {
	return func(next aoni.HTTPDoer) aoni.HTTPDoer {
		return aoni.DoerFunc(func(req *http.Request) (*http.Response, error) {
			if cfg.LatencyMax > cfg.LatencyMin && cfg.LatencyMin > 0 {
				diff := cfg.LatencyMax - cfg.LatencyMin

				r, err := rand.Int(rand.Reader, big.NewInt(int64(diff)))
				if err == nil {
					delay := cfg.LatencyMin + time.Duration(r.Int64())
					select {
					case <-req.Context().Done():
						return nil, req.Context().Err()
					case <-time.After(delay):
					}
				}
			} else if cfg.LatencyMin > 0 {
				select {
				case <-req.Context().Done():
					return nil, req.Context().Err()
				case <-time.After(cfg.LatencyMin):
				}
			}

			if cfg.FailureRate > 0 {
				r, err := rand.Int(rand.Reader, big.NewInt(10000))
				if err == nil {
					val := float64(r.Int64()) / 10000.0
					if val < cfg.FailureRate {
						return &http.Response{
							StatusCode: http.StatusServiceUnavailable,
							Status:     http.StatusText(http.StatusServiceUnavailable),
							Proto:      "HTTP/1.1",
							ProtoMajor: 1,
							ProtoMinor: 1,
							Header:     http.Header{"Content-Type": []string{"text/plain"}},
							Body:       io.NopCloser(strings.NewReader("aoni: simulated chaos network failure")),
							Request:    req,
						}, nil
					}
				}
			}

			return next.Do(req)
		})
	}
}

// AdaptiveLimit returns a [Middleware] that gates
// requests through limiter. Each request acquires a slot before
// execution and releases it afterward with the observed RTT.
func AdaptiveLimit(limiter *limiter.AdaptiveLimiter) aoni.Middleware {
	return func(next aoni.HTTPDoer) aoni.HTTPDoer {
		return aoni.DoerFunc(func(req *http.Request) (*http.Response, error) {
			if err := limiter.Acquire(req.Context()); err != nil {
				return nil, err
			}

			start := time.Now()
			resp, err := next.Do(req)
			rtt := time.Since(start)

			limiter.Release(rtt)

			return resp, err
		})
	}
}

func isFatalError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, aoni.ErrSSRFBlocked) || errors.Is(err, aoni.ErrRedirectDomainForbidden) {
		return true
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Err != nil && strings.Contains(urlErr.Err.Error(), "unsupported protocol scheme") {
			return true
		}
	}

	var (
		certInvalidErr  *x509.CertificateInvalidError
		unknownAuthErr  *x509.UnknownAuthorityError
		hostnameErr     *x509.HostnameError
		recordHeaderErr tls.RecordHeaderError
	)

	if errors.As(err, &certInvalidErr) ||
		errors.As(err, &unknownAuthErr) ||
		errors.As(err, &hostnameErr) ||
		errors.As(err, &recordHeaderErr) {
		return true
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, "certificate signed by unknown authority") ||
		strings.Contains(errMsg, "certificate pinning failure") {
		return true
	}

	return false
}

func parseRetryAfter(resp *http.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}

	val := resp.Header.Get("Retry-After")
	if val == "" {
		return 0, false
	}

	secs, err := strconv.ParseInt(val, 10, 64)
	if err == nil && secs >= 0 {
		maxSecs := int64(math.MaxInt64 / time.Second)
		if secs > maxSecs {
			secs = maxSecs
		}

		return time.Duration(secs) * time.Second, true
	} else if errors.Is(err, strconv.ErrRange) {
		maxSecs := int64(math.MaxInt64 / time.Second)
		return time.Duration(maxSecs) * time.Second, true
	}

	if t, err := http.ParseTime(val); err == nil {
		if delay := time.Until(t); delay > 0 {
			return delay, true
		}

		return 0, true
	}

	return 0, false
}

func maskQueryParams(u *url.URL) string {
	if u == nil {
		return ""
	}

	copy := *u

	q := copy.Query()
	for key := range q {
		if key == "key" || key == "access_token" || key == "token" {
			q.Set(key, "***")
		}
	}

	copy.RawQuery = q.Encode()

	return copy.String()
}
