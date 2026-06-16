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
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Middleware wraps an HTTPDoer with additional logic.
type Middleware func(next HTTPDoer) HTTPDoer

// Chain applies a series of middlewares to an HTTPDoer, returning the final HTTPDoer.
// The first middleware in the list will be the outermost one (called first).
//
// Example:
//
//	rotator, _ := aoni.NewProxyRotator(cfg, proxy1, proxy2)
//	logMiddleware := aoni.LoggingMiddleware(logger)
//
//	// Build a chain: Client -> Logging -> Proxy Rotator
//	httpClient := aoni.Chain(rotator, logMiddleware)
//	client := aoni.NewClient(httpClient)
func Chain(doer HTTPDoer, middlewares ...Middleware) HTTPDoer {
	for i := len(middlewares) - 1; i >= 0; i-- {
		doer = middlewares[i](doer)
	}

	return doer
}

// RateLimitMiddleware returns a middleware that limits the rate of requests.
// It uses a token bucket algorithm to control the request frequency.
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

// JitterStrategy defines the algorithm used to calculate retry delay noise.
type JitterStrategy int

const (
	// JitterEqual calculates sleep time as backoff plus a randomized +/- 10% jitter.
	JitterEqual JitterStrategy = iota
	// JitterFull calculates sleep time as a completely random value between 0 and backoff (AWS Full Jitter).
	JitterFull
)

// RetryOptions defines the configuration for the [RetryMiddleware].
type RetryOptions struct {
	// MaxRetries is the maximum number of attempts before giving up.
	MaxRetries uint32
	// Backoff is the initial delay duration before the first retry.
	Backoff time.Duration
	// JitterStrategy is the strategy used to calculate backoff noise.
	JitterStrategy JitterStrategy
}

// RetryCondition is a function that determines whether a request should be retried.
// It receives the response and error from the failed attempt.
// Return true to retry, false to return the result immediately.
type RetryCondition func(resp *http.Response, err error) bool

// RetryMiddleware returns a middleware that retries requests based on the provided condition.
// It automatically buffers the request body to allow multiple attempts.
func RetryMiddleware(opts RetryOptions, condition RetryCondition) Middleware {
	if opts.MaxRetries == 0 {
		opts.MaxRetries = 3
	}

	if opts.Backoff == 0 {
		opts.Backoff = 1 * time.Second
	}

	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			var (
				body []byte
				err  error
			)

			// Buffer body for potential retries
			if req.Body != nil && req.Body != http.NoBody {
				body, err = io.ReadAll(req.Body)
				if err != nil {
					return nil, fmt.Errorf("aoni: failed to read request body for retry: %w", err)
				}

				_ = req.Body.Close()
			}

			backoff := opts.Backoff

			for i := uint32(0); i <= opts.MaxRetries; i++ {
				// Re-create body reader for each attempt
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
					switch {
					case hasRetryAfter:
						sleepTime = retryAfter
					case opts.JitterStrategy == JitterFull:
						// AWS Full Jitter: Sleep = random(0, backoff)
						r, err := rand.Int(rand.Reader, big.NewInt(int64(backoff)))
						if err != nil {
							return nil, fmt.Errorf("aoni: failed to generate jitter: %w", err)
						}

						sleepTime = time.Duration(r.Int64())

					default:
						// JitterEqual: backoff + randomized +/- 10% of backoff
						r, err := rand.Int(rand.Reader, big.NewInt(int64(backoff/5)))
						if err != nil {
							return nil, fmt.Errorf("aoni: failed to generate jitter: %w", err)
						}

						jitter := time.Duration(r.Int64())
						sleepTime = backoff + (jitter - backoff/10)
					}

					select {
					case <-req.Context().Done():
						return nil, req.Context().Err()
					case <-time.After(sleepTime):
						backoff *= 2 // Exponential backoff
						continue
					}
				}

				return resp, err
			}

			return nil, errors.New("aoni: max retries exceeded")
		})
	}
}

// ProxyRetryCondition returns a RetryCondition that retries on proxy-related faults.
// It is a convenience wrapper for use with [ProxyRotator].
func ProxyRetryCondition(rotator *ProxyRotator) RetryCondition {
	return func(resp *http.Response, err error) bool {
		return rotator.isProxyFault(resp, err)
	}
}

// RecoveryMiddleware returns a middleware that catches any panic occurring during request execution.
// It executes the optional onPanic callback and returns the panic as a standard Go error.
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

// CircuitState represents the state of a circuit breaker.
type CircuitState int

// Possible circuit states.
const (
	StateClosed CircuitState = iota
	StateOpen
	StateHalfOpen
)

// CircuitBreakerConfig defines configuration for a CircuitBreaker.
type CircuitBreakerConfig struct {
	FailureThreshold uint32
	SuccessThreshold uint32
	Cooldown         time.Duration
}

type circuit struct {
	mu           sync.RWMutex
	state        CircuitState
	failCount    uint32
	successCount uint32
	lastStateChg time.Time
}

// CircuitBreaker implements the circuit breaker pattern per host.
type CircuitBreaker struct {
	cfg      CircuitBreakerConfig
	mu       sync.RWMutex
	circuits map[string]*circuit
}

// NewCircuitBreaker initializes a CircuitBreaker.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.FailureThreshold == 0 {
		cfg.FailureThreshold = 5
	}

	if cfg.SuccessThreshold == 0 {
		cfg.SuccessThreshold = 2
	}

	if cfg.Cooldown == 0 {
		cfg.Cooldown = 10 * time.Second
	}

	return &CircuitBreaker{
		cfg:      cfg,
		circuits: make(map[string]*circuit),
	}
}

func (cb *CircuitBreaker) getCircuit(host string) *circuit {
	cb.mu.RLock()
	c, ok := cb.circuits[host]
	cb.mu.RUnlock()

	if ok {
		return c
	}

	cb.mu.Lock()

	c, ok = cb.circuits[host]
	if !ok {
		c = &circuit{
			state:        StateClosed,
			lastStateChg: time.Now(),
		}
		cb.circuits[host] = c
	}

	cb.mu.Unlock()

	return c
}

func (c *circuit) allowRequestState(cooldown time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == StateClosed {
		return true
	}

	if c.state == StateOpen {
		if time.Since(c.lastStateChg) >= cooldown {
			c.state = StateHalfOpen
			c.successCount = 0
			c.lastStateChg = time.Now()

			return true
		}

		return false
	}

	return true
}

func (c *circuit) recordSuccess(successThreshold uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch c.state {
	case StateHalfOpen:
		c.successCount++
		if c.successCount >= successThreshold {
			c.state = StateClosed
			c.failCount = 0
			c.lastStateChg = time.Now()
		}

	case StateClosed:
		c.failCount = 0
	}
}

func (c *circuit) recordFailure(failureThreshold uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch c.state {
	case StateClosed:
		c.failCount++
		if c.failCount >= failureThreshold {
			c.state = StateOpen
			c.lastStateChg = time.Now()
		}

	case StateHalfOpen:
		c.state = StateOpen
		c.lastStateChg = time.Now()
	}
}

// DefaultCircuitBreakerCondition returns true for transport errors and 5xx status codes.
func DefaultCircuitBreakerCondition(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}

	if resp != nil {
		return resp.StatusCode >= 500
	}

	return false
}

// CircuitBreakerMiddleware prevents calling failing hosts by instantly rejecting requests
// once a failure threshold is hit.
func CircuitBreakerMiddleware(cb *CircuitBreaker, isFailure func(*http.Response, error) bool) Middleware {
	if isFailure == nil {
		isFailure = DefaultCircuitBreakerCondition
	}

	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			host := req.URL.Host
			c := cb.getCircuit(host)

			if !c.allowRequestState(cb.cfg.Cooldown) {
				return nil, fmt.Errorf("aoni: circuit breaker open for host %s", host)
			}

			resp, err := next.Do(req)

			if isFailure(resp, err) {
				c.recordFailure(cb.cfg.FailureThreshold)
			} else {
				c.recordSuccess(cb.cfg.SuccessThreshold)
			}

			return resp, err
		})
	}
}

// FallbackFunc is a function to provide fallback responses on failure.
type FallbackFunc func(req *http.Request, origErr error) (*http.Response, error)

type fallbackCtxKey struct{}

// WithFallback returns a RequestModifier that registers a FallbackFunc for a single request.
func WithFallback(f FallbackFunc) RequestModifier {
	return func(req *http.Request) {
		ctx := context.WithValue(req.Context(), fallbackCtxKey{}, f)
		*req = *req.WithContext(ctx)
	}
}

// FallbackJSON returns a FallbackFunc that generates a mock response with a JSON payload.
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

// FallbackMiddleware executes registered fallbacks on request failure.
func FallbackMiddleware() Middleware {
	return FallbackMiddlewareEx(nil)
}

// FallbackMiddlewareEx executes fallbacks on custom failure conditions.
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

// ChaosConfig defines parameters for simulating network instability.
type ChaosConfig struct {
	// FailureRate is the probability (from 0.0 to 1.0) of a request randomly failing with a 503 Service Unavailable.
	FailureRate float64
	// LatencyMin is the minimum artificial delay added to the request.
	LatencyMin time.Duration
	// LatencyMax is the maximum artificial delay added to the request.
	LatencyMax time.Duration
}

// ChaosMiddleware returns a middleware that simulates network latency and random server failures.
// This is extremely useful in testing environments to verify circuit breakers, retries, and fallback logic.
func ChaosMiddleware(cfg ChaosConfig) Middleware {
	return func(next HTTPDoer) HTTPDoer {
		return DoerFunc(func(req *http.Request) (*http.Response, error) {
			// 1. Add latency if configured
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

			// 2. Inject random failure if configured
			if cfg.FailureRate > 0 {
				r, err := rand.Int(rand.Reader, big.NewInt(10000))
				if err == nil {
					val := float64(r.Int64()) / 10000.0
					if val < cfg.FailureRate {
						// Create fake 503 Service Unavailable response
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

// AdaptiveLimiter implements Vegas-style adaptive concurrency limiting.
type AdaptiveLimiter struct {
	mu          sync.Mutex
	limit       float64
	minLimit    float64
	maxLimit    float64
	alpha       float64
	beta        float64
	active      int
	waitChs     []chan struct{}
	minRTT      time.Duration
	smoothedRTT time.Duration
	lastReset   time.Time
}

// NewAdaptiveLimiter initializes an AdaptiveLimiter with Vegas congestion control settings.
func NewAdaptiveLimiter(initialLimit float64) *AdaptiveLimiter {
	return &AdaptiveLimiter{
		limit:     initialLimit,
		minLimit:  1.0,
		maxLimit:  1000.0,
		alpha:     2.0,
		beta:      5.0,
		lastReset: time.Now(),
	}
}

// Acquire waits for a concurrency slot, or returns an error if the context is cancelled.
func (l *AdaptiveLimiter) Acquire(ctx context.Context) error {
	l.mu.Lock()
	if l.active < int(l.limit) {
		l.active++
		l.mu.Unlock()
		return nil
	}

	ch := make(chan struct{})
	l.waitChs = append(l.waitChs, ch)
	l.mu.Unlock()

	select {
	case <-ctx.Done():
		l.mu.Lock()
		for i, w := range l.waitChs {
			if w == ch {
				l.waitChs = append(l.waitChs[:i], l.waitChs[i+1:]...)
				break
			}
		}

		l.mu.Unlock()

		return ctx.Err()

	case <-ch:
		return nil
	}
}

// Release updates RTT statistics and adjusts the concurrency limit accordingly.
func (l *AdaptiveLimiter) Release(rtt time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.active--

	if time.Since(l.lastReset) > 30*time.Second {
		l.minRTT = 0
		l.lastReset = time.Now()
	}

	if l.minRTT == 0 || rtt < l.minRTT {
		l.minRTT = rtt
	}

	if l.smoothedRTT == 0 {
		l.smoothedRTT = rtt
	} else {
		l.smoothedRTT = time.Duration(0.9*float64(l.smoothedRTT) + 0.1*float64(rtt))
	}

	queue := l.limit * (1.0 - float64(l.minRTT)/float64(l.smoothedRTT))

	if queue > l.beta {
		l.limit = max(l.minLimit, l.limit-1.0)
	} else if queue < l.alpha {
		l.limit = min(l.maxLimit, l.limit+1.0)
	}

	slots := int(l.limit) - l.active
	for slots > 0 && len(l.waitChs) > 0 {
		ch := l.waitChs[0]
		l.waitChs = l.waitChs[1:]

		close(ch)

		l.active++
		slots--
	}
}

// Limit returns the current adaptive limit value.
func (l *AdaptiveLimiter) Limit() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit
}

// AdaptiveLimitMiddleware returns a middleware that controls concurrent request execution dynamically.
func AdaptiveLimitMiddleware(limiter *AdaptiveLimiter) Middleware {
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
