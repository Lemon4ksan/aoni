// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package aoni – per-request transport overrides.
//
// Every modifier in github.com/lemon4ksan/aoni/mod stores its value in the request context
// using an unexported key type (the "Context Accessors" pattern). The
// transport layer reads the value back with the matching Get* function.
// This avoids creating a new [Client] for each one-off tweak.

package aoni

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/miyako/generic"
)

// HostRewriteRules extracts and returns the active host rewrite rules map from the given context.
// Returns nil if no rules are configured in the context.
func HostRewriteRules(ctx context.Context) map[string]string {
	cfg := GetRequestConfig(ctx)
	if cfg != nil && cfg.HostRewrite != nil {
		return cfg.HostRewrite.Rules
	}

	return nil
}

// WithContextModifier returns a new context carrying the given RequestModifiers.
// Third-party libraries that pass context through [http.Request] will carry
// these modifiers into the aoni pipeline automatically.
//
// Example with go-resty:
//
//	ctx := WithContextModifier(context.Background(),
//	    WithHeader("X-Api-Key", "secret"),
//	    TraceJA4(info),
//	)
//	resp, err := restyClient.R().SetContext(ctx).Get("/api/data")
func WithContextModifier(ctx context.Context, mods ...RequestModifier) context.Context {
	if len(mods) == 0 {
		return ctx
	}

	cfg := GetRequestConfig(ctx)
	if cfg == nil {
		cfg = &RequestConfig{
			Metadata: make(map[string]any),
		}
		ctx = context.WithValue(ctx, requestConfigKey{}, cfg)
	}

	cfg.Modifiers = append(cfg.Modifiers, mods...)

	return ctx
}

// ContextModifiers extracts the RequestModifiers previously stored via
// [WithContextModifier]. Returns nil if none are present.
func ContextModifiers(ctx context.Context) []RequestModifier {
	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		return cfg.Modifiers
	}

	return nil
}

// MarkModifierError attaches an error to the request config.
// The [Client] will check for this error before dispatching the request
// and return it if present, preventing malformed requests from being sent.
func MarkModifierError(req *http.Request, err error) {
	if err == nil {
		return
	}

	GetOrInitRequestConfig(req).BodyError = err
}

// GetProxyOverride returns the per-request proxy URL stored by [mod.WithProxyOverride].
func GetProxyOverride(ctx context.Context) generic.Optional[string] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.ProxyAddr == nil {
		return generic.None[string]()
	}

	return generic.Some(cfg.ProxyAddr.String())
}

// GetInsecureSkipVerify reports whether the request carries a per-request
// InsecureSkipVerify flag set by [mod.WithInsecureSkipVerify].
func GetInsecureSkipVerify(ctx context.Context) bool {
	cfg := GetRequestConfig(ctx)
	return cfg != nil && cfg.InsecureSkipVerify
}

// TCPDelayRange describes a random jitter window applied before the TCP dial.
type TCPDelayRange struct {
	Min time.Duration
	Max time.Duration
}

// GetTCPDelay returns the [TCPDelayRange] stored by [mod.WithTCPDelay] and
// blocks for a random duration within that range. Callers inside a dialer
// should call this before opening the connection.
func GetTCPDelay(ctx context.Context) generic.Optional[TCPDelayRange] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.TCPDelay.Max <= 0 {
		return generic.None[TCPDelayRange]()
	}

	return generic.Some(cfg.TCPDelay)
}

// ApplyTCPDelay reads the delay range from ctx and sleeps for a uniformly
// distributed random duration within it. It respects context cancellation.
// Returns ctx.Err() if the context is cancelled during the sleep, nil otherwise.
func ApplyTCPDelay(ctx context.Context) error {
	r, ok := GetTCPDelay(ctx).Value()
	if !ok || r.Max <= 0 {
		return nil
	}

	window := r.Max - r.Min

	delay := r.Min
	if window > 0 {
		delay += time.Duration(rand.Int64N(int64(window))) //nolint:gosec
	}

	if delay <= 0 {
		return nil
	}

	select {
	case <-time.After(delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GetConnMetadata retrieves a value previously set by [mod.WithConnMetadata].
func GetConnMetadata(ctx context.Context, key string) generic.Optional[any] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.Metadata == nil {
		return generic.None[any]()
	}

	val, ok := cfg.Metadata[key]
	if !ok {
		return generic.None[any]()
	}

	return generic.Some(val)
}

// GetResponseValidator returns the per-request validator registered by
// [mod.WithResponseValidator], or nil when none is set.
func GetResponseValidator(ctx context.Context) func(resp *http.Response) error {
	cfg := GetRequestConfig(ctx)
	if cfg == nil {
		return nil
	}

	return cfg.ResponseValidator
}

// GetCacheTTL returns the per-request cache TTL set by [mod.WithCacheTTL].
func GetCacheTTL(ctx context.Context) generic.Optional[time.Duration] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.CacheTTL <= 0 {
		return generic.None[time.Duration]()
	}

	return generic.Some(cfg.CacheTTL)
}

// RetryCondition reports whether a failed request should be retried.
type RetryCondition func(resp *http.Response, err error) bool

// Or combines multiple [RetryCondition] functions into a single condition
// that returns true if ANY of the underlying conditions return true.
func Or(conditions ...RetryCondition) RetryCondition {
	return func(resp *http.Response, err error) bool {
		for _, cond := range conditions {
			if cond != nil && cond(resp, err) {
				return true
			}
		}

		return false
	}
}

// And combines multiple [RetryCondition] functions into a single condition
// that returns true if ALL of the underlying conditions return true.
func And(conditions ...RetryCondition) RetryCondition {
	return func(resp *http.Response, err error) bool {
		for _, cond := range conditions {
			if cond == nil || !cond(resp, err) {
				return false
			}
		}

		return true
	}
}

// FallbackFunc provides an alternate response when a request fails.
type FallbackFunc func(req *http.Request, origErr error) (*http.Response, error)

// FallbackString returns a [FallbackFunc] that responds with plain text and the given statusCode.
func FallbackString(statusCode int, text string) FallbackFunc {
	return func(req *http.Request, origErr error) (*http.Response, error) {
		return &http.Response{
			StatusCode:    statusCode,
			Status:        http.StatusText(statusCode),
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}},
			Body:          io.NopCloser(strings.NewReader(text)),
			ContentLength: int64(len(text)),
			Request:       req,
		}, nil
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

// RetryOverride holds per-request retry settings that override the client-level
// [RetryMiddleware] configuration.
type RetryOverride struct {
	// MaxAttempts is the maximum number of total attempts (including the first).
	// 1 means no retries; 0 is treated as 1 (no retries).
	MaxAttempts int
	// Backoff is the delay before the first retry. Subsequent retries use
	// exponential back-off starting from this value.
	Backoff time.Duration
	// Condition is called after each failed attempt to decide whether to retry.
	// When nil [RetryOnErr] is used as the default.
	Condition RetryCondition
}

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

			return strings.Contains(errStr, "connection refused") ||
				strings.Contains(errStr, "connection reset") ||
				strings.Contains(errStr, "broken pipe")
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

// GetRetryOverride returns the per-request [RetryOverride] set by [mod.WithRetryPolicy].
func GetRetryOverride(ctx context.Context) generic.Optional[RetryOverride] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.RetryPolicy == nil {
		return generic.None[RetryOverride]()
	}

	return generic.Some(*cfg.RetryPolicy)
}

// ProxyFuncWithOverride wraps a base proxy function (e.g. [http.ProxyURL] or
// [http.ProxyFromEnvironment]) so that a per-request [mod.WithProxyOverride] value
// takes precedence. Pass the result as [http.Transport.Proxy].
//
//	transport.Proxy = ProxyFuncWithOverride(http.ProxyFromEnvironment)
func ProxyFuncWithOverride(base func(*http.Request) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		if raw, ok := GetProxyOverride(req.Context()).Value(); ok && raw != "" {
			return url.Parse(raw)
		}

		if base != nil {
			return base(req)
		}

		return nil, nil
	}
}

// TLSConfigWithOverride clones base and applies any per-request overrides
// found in ctx (currently [mod.WithInsecureSkipVerify]). Returns base unchanged
// when no overrides are present so no unnecessary allocations occur.
func TLSConfigWithOverride(ctx context.Context, base *tls.Config) *tls.Config {
	if !GetInsecureSkipVerify(ctx) {
		return base
	}

	var cloned *tls.Config
	if base != nil {
		cloned = base.Clone()
	} else {
		cloned = &tls.Config{} //nolint:gosec
	}

	cloned.InsecureSkipVerify = true //nolint:gosec

	return cloned
}
