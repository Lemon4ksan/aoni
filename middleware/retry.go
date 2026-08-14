// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/internal/requestutil"
	"github.com/lemon4ksan/aoni/netutil/proxy"
)

var (
	// ErrMaxRetriesExceeded indicates that a request attempt failed after exhausting all retry attempts.
	ErrMaxRetriesExceeded = errors.New("aoni/middleware: max retries exceeded")

	// ErrRetryAfterExceeded is returned when the server's Retry-After delay exceeds opts.MaxRetryAfter.
	ErrRetryAfterExceeded = errors.New("aoni/middleware: server Retry-After exceeds MaxRetryAfter limit")
)

// RetryOnProxyFault is a convenience alias for [proxy.RetryCondition],
// triggering retries when the proxy rotator detects an exit node fault.
var RetryOnProxyFault = proxy.RetryCondition

// JitterStrategy defines randomized delay distribution algorithms for retries.
type JitterStrategy int

const (
	JitterNone JitterStrategy = iota
	JitterFull
	JitterEqual
)

// RetryOptions configures backoff, jitter, and idempotency constraints for request retries.
type RetryOptions struct {
	MaxAttempts        uint32
	MaxRetries         uint32
	InitialBackoff     time.Duration
	Backoff            time.Duration
	MaxBackoff         time.Duration
	BackoffFactor      float64
	Jitter             bool
	JitterStrategy     JitterStrategy
	HonorRetryAfter    bool
	MaxRetryAfter      time.Duration
	AsyncThreshold     uint32
	AutoIdempotencyKey bool
	AllowedMethods     []string
	OnRetry            func(attempt uint32, err error, delay time.Duration)
}

// RetryOnErr returns a [aoni.RetryCondition] that triggers a retry on any non-nil error.
func RetryOnErr() aoni.RetryCondition {
	return func(resp aoni.Response, err error) bool {
		return err != nil
	}
}

// RetryOnTransientErrors returns a [aoni.RetryCondition] triggering retries on network timeouts or 5xx server errors.
func RetryOnTransientErrors() aoni.RetryCondition {
	return func(resp aoni.Response, err error) bool {
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return true
			}

			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "connection reset") ||
				strings.Contains(errStr, "connection refused") ||
				strings.Contains(errStr, "broken pipe") ||
				strings.Contains(errStr, "timeout") {
				return true
			}

			return false
		}

		if resp != nil {
			status := resp.StatusCode()
			return status >= 500 && status <= 599
		}

		return false
	}
}

// RetryOnRateLimit returns a [aoni.RetryCondition] triggering retries on HTTP 429 Too Many Requests.
func RetryOnRateLimit() aoni.RetryCondition {
	return func(resp aoni.Response, err error) bool {
		return resp != nil && resp.StatusCode() == http.StatusTooManyRequests
	}
}

// RetryOnGatewayErrors returns a [aoni.RetryCondition] triggering retries on HTTP 502, 503, and 504.
func RetryOnGatewayErrors() aoni.RetryCondition {
	return func(resp aoni.Response, err error) bool {
		if resp == nil {
			return false
		}

		code := resp.StatusCode()

		return code == http.StatusBadGateway || code == http.StatusServiceUnavailable ||
			code == http.StatusGatewayTimeout
	}
}

// RetryOnGRPCStatus returns a [aoni.RetryCondition] triggering retries when gRPC trailer status matches codes.
func RetryOnGRPCStatus(statusCodes ...string) aoni.RetryCondition {
	return func(resp aoni.Response, err error) bool {
		var (
			codeStr string
			grpcErr *decode.GRPCWebError
		)

		if errors.As(err, &grpcErr) {
			codeStr = grpcErr.StatusCode
		} else if resp != nil {
			codeStr = resp.Header("grpc-status")
		}

		if codeStr != "" {
			if len(statusCodes) == 0 && codeStr != "0" {
				return true
			}

			if slices.Contains(statusCodes, codeStr) {
				return true
			}
		}

		return false
	}
}

// Retry constructs an [aoni.Middleware] executing automated retries with exponential backoff.
func Retry(opts RetryOptions, condition aoni.RetryCondition) aoni.Middleware {
	if opts.MaxAttempts == 0 && opts.MaxRetries > 0 {
		opts.MaxAttempts = opts.MaxRetries + 1
	}

	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 1
	}

	if opts.InitialBackoff == 0 && opts.Backoff > 0 {
		opts.InitialBackoff = opts.Backoff
	}

	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			ctx := req.Context()
			activeOpts, activeCond := resolveRetryOverrides(req, opts, condition)

			if activeOpts.AutoIdempotencyKey {
				ensureIdempotencyKey(req)
			}

			bo := createBackoffGenerator(activeOpts)

			var (
				lastResp aoni.Response
				lastErr  error
			)

			for attempt := uint32(1); attempt <= activeOpts.MaxAttempts; attempt++ {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
				}

				if attempt > 1 {
					if !allowRetryForMethod(req, activeOpts, lastResp) {
						break
					}

					sleepDur, exceeded := calculateRetrySleep(lastResp, bo, activeOpts)
					if exceeded {
						return lastResp, ErrRetryAfterExceeded
					}

					if activeOpts.OnRetry != nil {
						activeOpts.OnRetry(attempt-1, lastErr, sleepDur)
					}

					if err := waitRetryDelay(ctx, sleepDur, attempt, activeOpts.AsyncThreshold); err != nil {
						return nil, err
					}
				}

				resp, err := next.Do(req)
				lastResp = resp
				lastErr = err

				if err != nil && isFatalError(err) {
					return resp, err
				}

				if activeCond == nil || !activeCond(resp, err) {
					return resp, err
				}
			}

			if lastErr != nil {
				return lastResp, fmt.Errorf("%w: %w", ErrMaxRetriesExceeded, lastErr)
			}

			return lastResp, nil
		})
	}
}

// isIdempotentMethod reports whether the HTTP method is idempotent per RFC 9110 §9.2.2.
func isIdempotentMethod(method string) bool {
	return requestutil.IsIdempotentMethod(method)
}

// allowRetryForMethod determines whether the HTTP method is eligible for automated retries.
func allowRetryForMethod(req aoni.Request, opts RetryOptions, resp aoni.Response) bool {
	method := req.Method()

	if len(opts.AllowedMethods) > 0 {
		for _, m := range opts.AllowedMethods {
			if strings.EqualFold(m, method) {
				return true
			}
		}

		return false
	}

	if isIdempotentMethod(method) || req.Header("Idempotency-Key") != "" || req.Header("X-Request-ID") != "" {
		return true
	}

	if resp != nil && (resp.StatusCode() == http.StatusTooManyRequests || resp.StatusCode() == 425) {
		return true
	}

	cfg := aoni.GetRequestConfig(req.Context())
	if cfg != nil && cfg.AllowNonReadOnlyHedging {
		return true
	}

	return false
}

// ensureIdempotencyKey sets a 128-bit hex Idempotency-Key if not already present on the request.
func ensureIdempotencyKey(req aoni.Request) {
	if req.Header("Idempotency-Key") == "" && req.Header("X-Request-ID") == "" {
		var (
			b   [16]byte
			dst [32]byte
		)

		_, _ = rand.Read(b[:])
		hex.Encode(dst[:], b[:])
		req.SetHeader("Idempotency-Key", string(dst[:]))
	}
}

// waitRetryDelay pauses execution for the calculated backoff duration.
func waitRetryDelay(ctx context.Context, delay time.Duration, attempt, asyncThreshold uint32) error {
	if asyncThreshold > 0 && attempt >= asyncThreshold {
		return waitAsync(ctx, delay)
	}

	return waitSync(ctx, delay)
}

// waitSync blocks synchronously until delay elapses or ctx is canceled.
func waitSync(ctx context.Context, delay time.Duration) error {
	t := time.NewTimer(delay)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// waitAsync blocks until delay elapses or ctx is canceled.
func waitAsync(ctx context.Context, delay time.Duration) error {
	t := time.NewTimer(delay)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// resolveRetryOverrides extracts per-request retry configuration overrides from request context.
func resolveRetryOverrides(
	req aoni.Request,
	baseOpts RetryOptions,
	baseCond aoni.RetryCondition,
) (RetryOptions, aoni.RetryCondition) {
	cfg := aoni.GetRequestConfig(req.Context())
	if cfg == nil || cfg.RetryPolicy == nil {
		return baseOpts, baseCond
	}

	override := cfg.RetryPolicy
	opts := baseOpts

	if override.MaxAttempts > 0 {
		opts.MaxAttempts = uint32(override.MaxAttempts)
	}

	if override.Backoff > 0 {
		opts.InitialBackoff = override.Backoff
	}

	cond := baseCond
	if override.Condition != nil {
		cond = override.Condition
	}

	return opts, cond
}

// createBackoffGenerator instantiates a new exponential backoff generator with configured jitter strategy.
func createBackoffGenerator(opts RetryOptions) *generic.Backoff {
	factor := opts.BackoffFactor
	if factor <= 0 {
		factor = 2.0
	}

	initial := opts.InitialBackoff
	if initial == 0 && opts.Backoff > 0 {
		initial = opts.Backoff
	}

	jitterFactor := 0.0
	if opts.Jitter {
		jitterFactor = 0.5
	}

	switch opts.JitterStrategy {
	case JitterFull:
		jitterFactor = 0.5
	case JitterEqual:
		jitterFactor = 0.25
	}

	return generic.NewBackoff(initial, opts.MaxBackoff, factor, jitterFactor)
}

// calculateRetrySleep computes the pause duration for the next retry attempt, respecting Retry-After headers.
func calculateRetrySleep(resp aoni.Response, bo *generic.Backoff, opts RetryOptions) (time.Duration, bool) {
	if resp != nil {
		if dur, ok := parseRetryAfter(resp); ok {
			if opts.MaxRetryAfter > 0 && dur > opts.MaxRetryAfter {
				return 0, true
			}

			return dur, false
		}
	}

	return bo.Next(), false
}
