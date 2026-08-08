// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package middleware provides composable HTTP request and response execution interceptors.
package middleware

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"slices"
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
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/internal/timer"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/netutil/proxy"
)

var (
	// ErrMaxRetriesExceeded indicates that a request attempt failed after exhausting all retry attempts.
	ErrMaxRetriesExceeded = errors.New("aoni: max retries exceeded")

	// ErrChaosFailure is returned when the chaos middleware artificially drops a transaction.
	ErrChaosFailure = errors.New("aoni: simulated chaos network failure")

	// ErrSlidingWindowCanceled indicates that context was canceled while waiting for a rate limiter slot.
	ErrSlidingWindowCanceled = errors.New("aoni: sliding window rate limit wait canceled")

	// ErrCircuitOpen is returned when a circuit breaker blocks requests to an unhealthy host.
	ErrCircuitOpen = errors.New("aoni: circuit breaker open for target host")

	// ErrRetryAfterExceeded is returned when the server's Retry-After delay exceeds opts.MaxRetryAfter.
	ErrRetryAfterExceeded = errors.New("aoni: server Retry-After exceeds MaxRetryAfter limit")
)

// RetryOnProxyFault is a convenience alias for [proxy.RetryCondition],
// triggering retries when the proxy rotator detects an exit node fault.
var RetryOnProxyFault = proxy.RetryCondition

// Chain composes an execution engine with an ordered sequence of [aoni.Middleware] layers.
//
// Execution Order:
// Interceptors execute in left-to-right order (the first middleware wraps the second, and so on).
func Chain(doer any, middlewares ...aoni.Middleware) aoni.RequestDoer {
	var rd aoni.RequestDoer
	switch d := doer.(type) {
	case aoni.RequestDoer:
		rd = d
	case aoni.HTTPDoer:
		rd = aoni.NewHTTPDoerAdapter(d)
	default:
		panic("middleware: invalid doer type passed to Chain")
	}

	for i := len(middlewares) - 1; i >= 0; i-- {
		if middlewares[i] != nil {
			rd = middlewares[i](rd)
		}
	}

	return rd
}

// Log records structured execution telemetry for HTTP transactions using logger.
// Sensitive query parameters ("key", "token", "access_token") are automatically masked.
func Log(logger aoni.Logger) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			startTime := time.Now()
			resp, err := next.Do(req)

			logger.Info("http request",
				"method", req.Method(),
				"url", maskURLString(req.URL()),
				"duration", time.Since(startTime),
				"error", err,
			)

			return resp, err
		})
	}
}

// RateLimit caps outbound request frequency using a token bucket algorithm.
func RateLimit(requestsPerSecond float64, burst int) aoni.Middleware {
	rps := max(requestsPerSecond, 0)
	burstLimit := max(burst, 0)

	limiter := rate.NewLimiter(rate.Limit(rps), burstLimit)

	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			if err := limiter.Wait(req.Context()); err != nil {
				return nil, fmt.Errorf("aoni: rate limit wait failed: %w", err)
			}

			return next.Do(req)
		})
	}
}

// SlidingWindowLimiter maintains a zero-allocation ring-buffer sliding window rate limiter.
type SlidingWindowLimiter struct {
	mu         sync.Mutex
	timestamps []time.Time
	window     time.Duration
	limit      int
	head       int
	tail       int
	count      int
}

// NewSlidingWindowLimiter creates a ring-buffer [SlidingWindowLimiter].
func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	capacity := max(limit, 1)

	return &SlidingWindowLimiter{
		limit:      capacity,
		window:     window,
		timestamps: make([]time.Time, capacity),
	}
}

// Allow checks whether a request attempt is permitted at timestamp now.
func (l *SlidingWindowLimiter) Allow(now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-l.window)

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

	oldest := l.timestamps[l.head]
	return false, oldest.Add(l.window).Sub(now)
}

// LimitEnforcer creates a [aoni.Middleware] enforcing limits via [SlidingWindowLimiter].
func LimitEnforcer(limiter *SlidingWindowLimiter) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			for {
				allowed, wait := limiter.Allow(time.Now())
				if allowed {
					return next.Do(req)
				}

				t := timer.Acquire(wait)
				select {
				case <-req.Context().Done():
					timer.Release(t)
					return nil, ErrSlidingWindowCanceled
				case <-t.C:
					timer.Release(t)
				}
			}
		})
	}
}

// JitterStrategy defines how randomized jitter is added to retry delays.
type JitterStrategy int

const (
	// JitterEqual splits backoff into a deterministic base and a randomized component.
	JitterEqual JitterStrategy = iota

	// JitterFull selects a random duration between zero and the computed delay cap.
	JitterFull
)

// RetryOptions configures backoff behavior, repetition bounds, and async starvation guards for [Retry].
type RetryOptions struct {
	OnRetry            func(attempt uint32, err error, delay time.Duration)
	Backoff            time.Duration
	MaxBackoff         time.Duration
	MaxRetryAfter      time.Duration
	MaxRetries         uint32
	AsyncThreshold     uint32
	JitterStrategy     JitterStrategy
	RetryNonIdempotent bool
}

// RetryOnErr triggers retries on any non-nil execution error.
func RetryOnErr() aoni.RetryCondition {
	return func(_ aoni.Response, err error) bool {
		return err != nil
	}
}

// RetryOnTransientErrors triggers retries on socket timeouts, connection resets, or broken pipes.
func RetryOnTransientErrors() aoni.RetryCondition {
	return func(_ aoni.Response, err error) bool {
		if err == nil {
			return false
		}

		var netErr net.Error
		if errors.As(err, &netErr) {
			return true
		}

		errStr := strings.ToLower(err.Error())

		return strings.Contains(errStr, "connection refused") ||
			strings.Contains(errStr, "connection reset") ||
			strings.Contains(errStr, "broken pipe")
	}
}

// RetryOnRateLimit triggers retries on HTTP 429 Too Many Requests status codes.
func RetryOnRateLimit() aoni.RetryCondition {
	return func(resp aoni.Response, _ error) bool {
		return resp != nil && resp.StatusCode() == http.StatusTooManyRequests
	}
}

// RetryOnGatewayErrors triggers retries on HTTP 502, 503, and 504 gateway status codes.
func RetryOnGatewayErrors() aoni.RetryCondition {
	return func(resp aoni.Response, _ error) bool {
		if resp == nil {
			return false
		}

		code := resp.StatusCode()

		return code == http.StatusBadGateway ||
			code == http.StatusServiceUnavailable ||
			code == http.StatusGatewayTimeout
	}
}

// RetryOnGRPCStatus triggers retries when gRPC-Web status matches candidate error codes.
func RetryOnGRPCStatus(statusCodes ...string) aoni.RetryCondition {
	targets := statusCodes
	if len(targets) == 0 {
		targets = []string{"14", "13", "8"}
	}

	return func(_ aoni.Response, err error) bool {
		if err == nil {
			return false
		}

		var grpcErr *decode.GRPCWebError

		return errors.As(err, &grpcErr) && slices.Contains(targets, grpcErr.StatusCode)
	}
}

// Retry automatically re-executes failed transactions up to opts.MaxRetries times.
// Switches to asynchronous time.AfterFunc dispatch if attempts exceed opts.AsyncThreshold
// to prevent Go runtime goroutine scheduler priority starvation.
func Retry(opts RetryOptions, condition aoni.RetryCondition) aoni.Middleware {
	opts.MaxRetries = generic.Coalesce(opts.MaxRetries, 3)
	opts.Backoff = max(generic.Coalesce(opts.Backoff, 1*time.Second), 0)
	opts.MaxBackoff = generic.Coalesce(opts.MaxBackoff, opts.Backoff*32)
	opts.MaxRetryAfter = generic.Coalesce(opts.MaxRetryAfter, 60*time.Second)
	opts.AsyncThreshold = generic.Coalesce(opts.AsyncThreshold, 25)

	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			activeOpts, activeCond := resolveRetryOverrides(req.Context(), opts, condition)
			isIdempotent := isIdempotentMethod(req.Method())

			var (
				bufferedBytes []byte
				bufferErr     error
				lastResp      aoni.Response
			)

			bodyBytes := req.BodyBytes()
			if len(bodyBytes) == 0 && req.BodyStream() != nil {
				bufferedBytes, bufferErr = io.ReadAll(req.BodyStream())
				if bufferErr != nil {
					return nil, fmt.Errorf("aoni: failed to read request body for retry: %w", bufferErr)
				}

				req.SetBodyBytes(bufferedBytes)
			}

			backoff := createBackoffGenerator(activeOpts)

			for attempt := uint32(0); attempt <= activeOpts.MaxRetries; attempt++ {
				if attempt > 0 {
					if !allowRetryForMethod(req, activeOpts, lastResp) {
						break
					}

					if !isIdempotent && activeOpts.RetryNonIdempotent {
						ensureIdempotencyKey(req)
					}

					if len(bufferedBytes) > 0 {
						req.SetBodyBytes(bufferedBytes)
					} else if len(bodyBytes) > 0 {
						req.SetBodyBytes(bodyBytes)
					}
				}

				resp, err := next.Do(req)
				lastResp = resp
				if attempt == activeOpts.MaxRetries || !activeCond(resp, err) {
					return resp, err
				}

				if !allowRetryForMethod(req, activeOpts, resp) {
					return resp, err
				}

				if err != nil && isFatalError(err) {
					return resp, err
				}

				sleepDuration, exceeded := calculateRetrySleep(resp, backoff, activeOpts)
				if exceeded {
					return resp, ErrRetryAfterExceeded
				}

				if activeOpts.OnRetry != nil {
					activeOpts.OnRetry(attempt+1, err, sleepDuration)
				}

				if waitErr := waitRetryDelay(
					req.Context(),
					sleepDuration,
					attempt+1,
					activeOpts.AsyncThreshold,
				); waitErr != nil {
					return nil, waitErr
				}
			}

			return nil, ErrMaxRetriesExceeded
		})
	}
}

func isIdempotentMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func allowRetryForMethod(req aoni.Request, opts RetryOptions, resp aoni.Response) bool {
	if isIdempotentMethod(req.Method()) || opts.RetryNonIdempotent {
		return true
	}
	if req.Header("Idempotency-Key") != "" || req.Header("X-Request-ID") != "" {
		return true
	}
	if resp != nil && (resp.Header("X-Proxy-Fault") != "" || resp.StatusCode() == http.StatusBadGateway) {
		return true
	}
	return false
}

func ensureIdempotencyKey(req aoni.Request) {
	if req.Header("Idempotency-Key") == "" && req.Header("X-Request-ID") == "" {
		var randBuf [16]byte
		_, _ = rand.Read(randBuf[:])
		key := fmt.Sprintf("%x-%x-%x-%x-%x", randBuf[0:4], randBuf[4:6], randBuf[6:8], randBuf[8:10], randBuf[10:])
		req.SetHeader("Idempotency-Key", key)
	}
}

func waitRetryDelay(ctx context.Context, delay time.Duration, attempt, asyncThreshold uint32) error {
	if asyncThreshold > 0 && attempt >= asyncThreshold {
		return waitAsync(ctx, delay)
	}

	return waitSync(ctx, delay)
}

func waitSync(ctx context.Context, delay time.Duration) error {
	t := timer.Acquire(delay)
	defer timer.Release(t)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func waitAsync(ctx context.Context, delay time.Duration) error {
	done := make(chan struct{})
	t := time.AfterFunc(delay, func() {
		close(done)
	})

	select {
	case <-ctx.Done():
		if !t.Stop() {
			<-done
		}

		return ctx.Err()

	case <-done:
		return nil
	}
}

func resolveRetryOverrides(
	ctx context.Context,
	baseOpts RetryOptions,
	baseCond aoni.RetryCondition,
) (RetryOptions, aoni.RetryCondition) {
	override, ok := aoni.GetRetryOverride(ctx).Value()
	if !ok {
		return baseOpts, baseCond
	}

	if override.MaxAttempts > 0 {
		baseOpts.MaxRetries = uint32(override.MaxAttempts - 1) //nolint:gosec
	}

	if override.Backoff > 0 {
		baseOpts.Backoff = override.Backoff
	}

	if override.Condition != nil {
		baseCond = override.Condition
	}

	return baseOpts, baseCond
}

func createBackoffGenerator(opts RetryOptions) *generic.Backoff {
	maxBackoff := generic.Coalesce(opts.MaxBackoff, opts.Backoff*32)
	if opts.JitterStrategy == JitterFull {
		return generic.NewBackoff(opts.Backoff, maxBackoff, 2, 1.0)
	}

	return generic.NewBackoff(opts.Backoff, maxBackoff, 2, 0.5)
}

func calculateRetrySleep(resp aoni.Response, bo *generic.Backoff, opts RetryOptions) (time.Duration, bool) {
	retryAfter, hasRetryAfter := parseRetryAfter(resp)
	if resp != nil {
		_ = resp.Close()
	}

	if hasRetryAfter {
		if opts.MaxRetryAfter > 0 && retryAfter > opts.MaxRetryAfter {
			return 0, true
		}
		return retryAfter, false
	}

	return bo.Next(), false
}

// Recover catches panics occurring during request execution and converts them to structured error instances.
func Recover(onPanic func(any)) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (resp aoni.Response, err error) {
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

// CircuitBreakerConfig configures health thresholds for [CircuitBreaker].
type CircuitBreakerConfig struct {
	Cooldown         time.Duration
	Window           time.Duration
	FailureThreshold float64
	MinRequests      int
}

// CircuitBreaker maintains per-host health state using a sliding failure window.
type CircuitBreaker struct {
	breakers map[string]*breaker.CircuitBreaker[any]
	km       keylock.KeyMutex[string]
	cfg      CircuitBreakerConfig
	mu       sync.Mutex
}

// NewCircuitBreaker initializes a per-host [CircuitBreaker].
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
	defer cb.mu.Unlock()

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

	return b
}

// DefaultCircuitBreakerCondition reports true for network errors or HTTP 5xx server responses.
func DefaultCircuitBreakerCondition(resp aoni.Response, err error) bool {
	if err != nil {
		return true
	}

	return resp != nil && resp.StatusCode() >= http.StatusInternalServerError
}

// CircuitBreak protects target hosts using sliding-window error monitoring.
//
// Streaming Safety:
// Response bodies are not consumed into RAM during status evaluation, maintaining zero-copy streaming integrity.
func CircuitBreak(cb *CircuitBreaker, isFailure func(aoni.Response, error) bool) aoni.Middleware {
	failureCheck := isFailure
	if failureCheck == nil {
		failureCheck = DefaultCircuitBreakerCondition
	}

	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			host := parseHostFromURL(req.URL())
			b := cb.getBreaker(host)

			var resultResp aoni.Response

			_, breakerErr := b.Do(req.Context(), func(ctx context.Context) (any, error) {
				req.SetContext(ctx)

				resp, err := next.Do(req)
				if err != nil {
					return nil, err
				}

				resultResp = resp

				if failureCheck(resp, nil) {
					return nil, fmt.Errorf("aoni: circuit breaker recorded failure (status %d)", resp.StatusCode())
				}

				return nil, nil
			})

			if errors.Is(breakerErr, breaker.ErrCircuitOpen) {
				return nil, fmt.Errorf("aoni: circuit breaker open for host %s: %w", host, ErrCircuitOpen)
			}

			if resultResp != nil {
				return resultResp, nil
			}

			return nil, breakerErr
		})
	}
}

// Fallback invokes a request-scoped [aoni.FallbackFunc] when request execution fails.
func Fallback() aoni.Middleware {
	return FallbackEx(nil)
}

// FallbackEx invokes a request-scoped [aoni.FallbackFunc] when isFailure evaluates to true.
func FallbackEx(isFailure func(aoni.Response, error) bool) aoni.Middleware {
	failureCheck := isFailure
	if failureCheck == nil {
		failureCheck = func(_ aoni.Response, err error) bool { return err != nil }
	}

	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			resp, err := next.Do(req)
			if !failureCheck(resp, err) {
				return resp, err
			}

			cfg := aoni.GetRequestConfig(req.Context())
			if cfg == nil || cfg.Fallback == nil {
				return resp, err
			}

			if cfg.Fallback == nil {
				return resp, err
			}

			fallbackResp, fallbackErr := cfg.Fallback(req, err)
			if fallbackErr != nil {
				return resp, err
			}

			if resp != nil {
				_ = resp.Close()
			}

			return fallbackResp, nil
		})
	}
}

// ChaosConfig configures fault rates and synthetic latency bounds for [Chaos].
type ChaosConfig struct {
	LatencyMin  time.Duration
	LatencyMax  time.Duration
	FailureRate float64
}

// Chaos injects random network latency and synthetic HTTP 503 errors.
func Chaos(cfg ChaosConfig) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			if err := applyChaosDelay(req.Context(), cfg); err != nil {
				return nil, err
			}

			if shouldInjectChaosError(cfg.FailureRate) {
				header := make(http.Header)
				header.Set("Content-Type", "text/plain")

				var httpReq *http.Request
				if req != nil {
					httpReq = req.HTTPRequest()
				}

				return aoni.NewStdResponse(&http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Status:     http.StatusText(http.StatusServiceUnavailable),
					Proto:      "HTTP/1.1",
					ProtoMajor: 1,
					ProtoMinor: 1,
					Header:     header,
					Body:       io.NopCloser(strings.NewReader("aoni: simulated chaos network failure")),
					Request:    httpReq,
				}), nil
			}

			return next.Do(req)
		})
	}
}

func applyChaosDelay(ctx context.Context, cfg ChaosConfig) error {
	var delay time.Duration

	if cfg.LatencyMax > cfg.LatencyMin && cfg.LatencyMin > 0 {
		diff := cfg.LatencyMax - cfg.LatencyMin

		r, err := rand.Int(rand.Reader, big.NewInt(int64(diff)))
		if err == nil {
			delay = cfg.LatencyMin + time.Duration(r.Int64())
		}
	} else if cfg.LatencyMin > 0 {
		delay = cfg.LatencyMin
	}

	if delay <= 0 {
		return nil
	}

	t := timer.Acquire(delay)
	select {
	case <-ctx.Done():
		timer.Release(t)
		return ctx.Err()
	case <-t.C:
		timer.Release(t)
		return nil
	}
}

func shouldInjectChaosError(rate float64) bool {
	if rate <= 0 {
		return false
	}

	r, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return false
	}

	return (float64(r.Int64()) / 10000.0) < rate
}

// AdaptiveLimit restricts concurrency using adaptive response-time feedback loops via [limiter.AdaptiveLimiter].
func AdaptiveLimit(limiter *limiter.AdaptiveLimiter) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			if err := limiter.Acquire(req.Context()); err != nil {
				return nil, err
			}

			startTime := time.Now()
			resp, err := next.Do(req)
			rtt := time.Since(startTime)

			limiter.Release(rtt)

			return resp, err
		})
	}
}

// GRPCWebTimeout attaches the standard `grpc-timeout` header to outgoing requests.
func GRPCWebTimeout(d time.Duration) aoni.Middleware {
	timeoutStr := formatGRPCTimeout(d)

	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			req.SetHeader("grpc-timeout", timeoutStr)
			return next.Do(req)
		})
	}
}

// GRPCMetadata attaches gRPC key-value metadata to outgoing request headers.
func GRPCMetadata(md map[string]string) aoni.Middleware {
	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			for k, v := range md {
				req.SetHeader(k, v)
			}

			return next.Do(req)
		})
	}
}

func isFatalError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, netdial.ErrSSRFBlocked) || errors.Is(err, aoni.ErrRedirectDomainForbidden) {
		return true
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil &&
		strings.Contains(urlErr.Err.Error(), "unsupported protocol scheme") {
		return true
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

	return strings.Contains(errMsg, "certificate signed by unknown authority") ||
		strings.Contains(errMsg, "certificate pinning failure")
}

func parseRetryAfter(resp aoni.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}

	val := resp.Header("Retry-After")
	if val == "" {
		return 0, false
	}

	secs, err := strconv.ParseInt(val, 10, 64)
	if err == nil && secs >= 0 {
		maxSecs := int64(math.MaxInt64 / time.Second)
		secs = min(secs, maxSecs)

		return time.Duration(secs) * time.Second, true
	}

	if errors.Is(err, strconv.ErrRange) {
		maxSecs := int64(math.MaxInt64 / time.Second)
		return time.Duration(maxSecs) * time.Second, true
	}

	if t, err := http.ParseTime(val); err == nil {
		return max(time.Until(t), 0), true
	}

	return 0, false
}

func parseHostFromURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	return u.Host
}

func maskURLString(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	return maskQueryParams(u)
}

func maskQueryParams(u *url.URL) string {
	if u == nil {
		return ""
	}

	copyURL := *u
	query := copyURL.Query()

	for key := range query {
		if key == "key" || key == "access_token" || key == "token" {
			query.Set(key, "***")
		}
	}

	copyURL.RawQuery = query.Encode()

	return copyURL.String()
}

func formatGRPCTimeout(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}

	switch {
	case d < time.Second:
		return strconv.FormatInt(d.Milliseconds(), 10) + "m"
	case d < time.Minute:
		return strconv.FormatInt(int64(d.Seconds()), 10) + "S"
	case d < time.Hour:
		return strconv.FormatInt(int64(d.Minutes()), 10) + "M"
	default:
		return strconv.FormatInt(int64(d.Hours()), 10) + "H"
	}
}
