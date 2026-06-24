// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/sync/breaker"
	"github.com/lemon4ksan/miyako/sync/keylock"
	"github.com/lemon4ksan/miyako/sync/limiter"
	"golang.org/x/time/rate"
)

// Middleware wraps an [HTTPDoer] with additional request/response
// processing logic. Pass to [Chain] to compose multiple layers.
type Middleware func(next HTTPDoer) HTTPDoer

// Chain wraps doer with middlewares from left to right: the first
// middleware in the slice executes first. Returns doer unmodified
// when middlewares is empty.
func Chain(doer HTTPDoer, middlewares ...Middleware) HTTPDoer {
	for i := len(middlewares) - 1; i >= 0; i-- {
		doer = middlewares[i](doer)
	}

	return doer
}

// RateLimitMiddleware returns a [Middleware] that blocks when the
// request rate exceeds rps with burst tolerance. The limiter uses
// a token bucket algorithm from [golang.org/x/time/rate].
func RateLimitMiddleware(rps float64, burst int) Middleware {
	limiter := rate.NewLimiter(rate.Limit(rps), burst)

	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			if err := limiter.Wait(req.Context()); err != nil {
				return nil, fmt.Errorf("aoni: rate limit wait failed: %w", err)
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

// RetryOptions configures [RetryMiddleware].
type RetryOptions struct {
	// MaxRetries is the total number of attempts (1 = no retries).
	MaxRetries uint32

	// Backoff is the delay before the first retry. Subsequent retries
	// use exponential backoff starting from this value.
	Backoff time.Duration

	// JitterStrategy selects the noise algorithm applied to each delay.
	JitterStrategy JitterStrategy
}

// RetryCondition reports whether a failed request should be retried.
type RetryCondition func(resp *http.Response, err error) bool

// RetryOnErr returns a [RetryCondition] that retries on any non-nil error.
func RetryOnErr() RetryCondition {
	return func(resp *http.Response, err error) bool {
		return err != nil
	}
}

// RetryOnTransientErrors returns a [RetryCondition] that retries on
// network errors, connection resets, and broken pipes.
func RetryOnTransientErrors() RetryCondition {
	return func(resp *http.Response, err error) bool {
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) {
				return true
			}

			errStr := err.Error()
			if strings.Contains(errStr, "connection refused") ||
				strings.Contains(errStr, "connection reset") ||
				strings.Contains(errStr, "broken pipe") {
				return true
			}

			return true
		}

		return false
	}
}

// RetryOnRateLimit returns a [RetryCondition] that retries on HTTP 429.
func RetryOnRateLimit() RetryCondition {
	return func(resp *http.Response, err error) bool {
		return resp != nil && resp.StatusCode == http.StatusTooManyRequests
	}
}

// RetryOnGatewayErrors returns a [RetryCondition] that retries on
// HTTP 502, 503, and 504 status codes.
func RetryOnGatewayErrors() RetryCondition {
	return func(resp *http.Response, err error) bool {
		if resp != nil {
			sc := resp.StatusCode
			return sc == http.StatusBadGateway || sc == http.StatusServiceUnavailable || sc == http.StatusGatewayTimeout
		}

		return false
	}
}

// RetryMiddleware returns a [Middleware] that retries requests
// matching condition up to opts.MaxRetries times. The request
// body is buffered in memory so it can be replayed. The middleware
// respects the Retry-After header when present and falls back to
// exponential backoff with jitter.
func RetryMiddleware(opts RetryOptions, condition RetryCondition) Middleware {
	opts.MaxRetries = generic.Coalesce(opts.MaxRetries, 3)
	opts.Backoff = generic.Coalesce(opts.Backoff, 1*time.Second)

	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
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
			switch opts.JitterStrategy {
			case JitterFull:
				bo = generic.NewBackoff(opts.Backoff, opts.Backoff*32, 2, 1.0)
			default:
				bo = generic.NewBackoff(opts.Backoff, opts.Backoff*32, 2, 0.5)
			}

			for i := uint32(0); i <= opts.MaxRetries; i++ {
				if body != nil {
					req.Body = io.NopCloser(bytes.NewReader(body))
				}

				resp, err := next.Do(req)

				if i < opts.MaxRetries && condition(resp, err) {
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

// ProxyRetryCondition returns a [RetryCondition] that retries when
// rotator considers the response or error a proxy fault.
func ProxyRetryCondition(rotator *ProxyRotator) RetryCondition {
	return func(resp *http.Response, err error) bool {
		return rotator.isProxyFault(resp, err)
	}
}

// RecoveryMiddleware catches panics during request execution, calls
// onPanic with the recovered value (if non-nil), and returns an error.
func RecoveryMiddleware(onPanic func(any)) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (resp *http.Response, err error) {
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
	if cfg.FailureThreshold <= 0 || cfg.FailureThreshold > 1.0 {
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

// DefaultCircuitBreakerCondition returns true for network errors and
// HTTP status codes >= 500.
func DefaultCircuitBreakerCondition(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}

	if resp != nil {
		return resp.StatusCode >= 500
	}

	return false
}

// CircuitBreakerMiddleware returns a [Middleware] that gates
// requests through cb per host. When the circuit is open the
// request fails immediately with an error. isFailure determines
// which responses count as failures; nil uses
// [DefaultCircuitBreakerCondition].
//
// The circuit breaker uses a sliding window: failures are tracked
// over [CircuitBreakerConfig.Window] and compared against
// [CircuitBreakerConfig.FailureThreshold] ratio.
func CircuitBreakerMiddleware(cb *CircuitBreaker, isFailure func(*http.Response, error) bool) Middleware {
	if isFailure == nil {
		isFailure = DefaultCircuitBreakerCondition
	}

	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
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

				body, _ := io.ReadAll(resp.Body)
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

// FallbackFunc provides an alternate response when a request fails.
type FallbackFunc func(req *http.Request, origErr error) (*http.Response, error)

// WithFallback returns a [RequestModifier] that registers f as the
// fallback for this request. See [FallbackMiddleware].
func WithFallback(f FallbackFunc) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), fallbackCtxKey{}, f)
		*req = *req.WithContext(ctx)
	}
}

// FallbackJSON returns a [FallbackFunc] that responds with data
// serialized as JSON and the given statusCode.
func FallbackJSON(statusCode int, data any) FallbackFunc {
	return func(req *http.Request, origErr error) (*http.Response, error) {
		bodyBytes, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}

		return &http.Response{
			StatusCode:    statusCode,
			Status:        http.StatusText(statusCode),
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        http.Header{"Content-Type": []string{"application/json"}},
			Body:          io.NopCloser(bytes.NewReader(bodyBytes)),
			ContentLength: int64(len(bodyBytes)),
			Request:       req,
		}, nil
	}
}

// FallbackMiddleware returns a [Middleware] that invokes the
// [FallbackFunc] registered via [WithFallback] when the request
// fails with any error.
func FallbackMiddleware() Middleware {
	return FallbackMiddlewareEx(nil)
}

// FallbackMiddlewareEx is like [FallbackMiddleware] but uses isFailure
// to decide which responses trigger the fallback. When isFailure is
// nil, any non-nil error triggers it.
func FallbackMiddlewareEx(isFailure func(*http.Response, error) bool) Middleware {
	if isFailure == nil {
		isFailure = func(resp *http.Response, err error) bool { return err != nil }
	}

	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			resp, err := next.Do(req)
			if isFailure(resp, err) {
				if f, ok := req.Context().Value(fallbackCtxKey{}).(FallbackFunc); ok && f != nil {
					if resp != nil {
						_ = resp.Body.Close()
					}

					fallbackResp, fallbackErr := f(req, err)
					if fallbackErr == nil {
						return fallbackResp, nil
					}
				}
			}

			return resp, err
		})
	}
}

func parseRetryAfter(resp *http.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}

	val := resp.Header.Get("Retry-After")
	if val == "" {
		return 0, false
	}

	if secs, err := strconv.ParseInt(val, 10, 64); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}

	if t, err := http.ParseTime(val); err == nil {
		if delay := time.Until(t); delay > 0 {
			return delay, true
		}

		return 0, true
	}

	return 0, false
}

// ChaosConfig defines parameters for [ChaosMiddleware].
type ChaosConfig struct {
	// FailureRate is the probability (0.0 to 1.0) of randomly returning a 503 error.
	FailureRate float64
	// LatencyMin is the minimum artificial delay duration applied to requests.
	LatencyMin time.Duration
	// LatencyMax is the maximum artificial delay duration applied to requests.
	LatencyMax time.Duration
}

// ChaosMiddleware returns a [Middleware] that injects random latency
// and 503 errors. Useful for testing retry and circuit breaker logic.
func ChaosMiddleware(cfg ChaosConfig) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
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

// AdaptiveLimitMiddleware returns a [Middleware] that gates
// requests through limiter. Each request acquires a slot before
// execution and releases it afterward with the observed RTT.
func AdaptiveLimitMiddleware(limiter *limiter.AdaptiveLimiter) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
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
