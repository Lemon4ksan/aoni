// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	stdio "io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/internal/timer"
	"github.com/lemon4ksan/aoni/telemetry"
)

// AsReplayable wraps rc into a [io.ReplayableBody] using active buffers or tee-buffered fallback.
var AsReplayable = io.AsReplayable

// ResponseTrace extracts the [TraceInfo] previously captured via [WithTraceContext].
// Returns nil if no trace was registered on the request.
func ResponseTrace(resp *http.Response) *telemetry.TraceInfo {
	if resp == nil || resp.Request == nil {
		return nil
	}

	cfg := GetRequestConfig(resp.Request.Context())
	if cfg != nil {
		return cfg.TraceInfo
	}

	return nil
}

// HostRewriteRules extracts and returns active host rewrite rules from the context.
// Returns nil if no rewrite rules are attached to the context.
func HostRewriteRules(ctx context.Context) map[string]string {
	cfg := GetRequestConfig(ctx)
	if cfg != nil && cfg.HostRewrite != nil {
		return cfg.HostRewrite.Rules
	}

	return nil
}

// WithContextModifier attaches request modifiers to the context.
// Third-party HTTP libraries carrying this context will propagate these modifiers
// into the aoni execution pipeline automatically.
func WithContextModifier(ctx context.Context, mods ...RequestModifier) context.Context {
	if len(mods) == 0 {
		return ctx
	}

	cfg := GetRequestConfig(ctx)
	if cfg == nil {
		ctx, cfg = AllocRequestConfig(ctx)
	}

	cfg.Modifiers = append(cfg.Modifiers, mods...)

	return ctx
}

// ContextModifiers retrieves all [RequestModifier] functions stored in the context.
// Note: returns pipeline.RequestModifier slice since modifiers are stored wrapped.
func ContextModifiers(ctx context.Context) []pipeline.RequestModifier {
	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		return cfg.Modifiers
	}

	return nil
}

// MarkModifierError attaches a serialization or setup error to the request config,
// causing the client to abort request dispatching before sending wire data.
func MarkModifierError(req any, err error) {
	if err == nil {
		return
	}

	GetOrInitRequestConfig(req).BodyError = err
}

// GetProxyOverride returns the per-request proxy URL string stored in the context.
func GetProxyOverride(ctx context.Context) generic.Optional[string] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.ProxyAddr == nil {
		return generic.None[string]()
	}

	return generic.Some(cfg.ProxyAddr.String())
}

// GetInsecureSkipVerify reports whether TLS certificate verification is bypassed for this request.
func GetInsecureSkipVerify(ctx context.Context) bool {
	cfg := GetRequestConfig(ctx)
	return cfg != nil && cfg.InsecureSkipVerify
}

// TCPDelayRange defines the bounds for randomized pre-dial TCP delay jitter.
type TCPDelayRange = pipeline.TCPDelayRange

// GetTCPDelay retrieves the configured [TCPDelayRange] from context.
func GetTCPDelay(ctx context.Context) generic.Optional[TCPDelayRange] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.TCPDelay.Max <= 0 {
		return generic.None[TCPDelayRange]()
	}

	return generic.Some(cfg.TCPDelay)
}

// ApplyTCPDelay inspects the context for TCP delay range jitter and sleeps
// for a random duration within those bounds before dialing.
//
// Respects context cancellation and recycles timer instances via the internal pool.
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

	t := timer.Acquire(delay)
	select {
	case <-t.C:
		timer.Release(t)
		return nil
	case <-ctx.Done():
		timer.Release(t)
		return ctx.Err()
	}
}

// GetConnMetadata retrieves connection metadata stored under key from context.
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

// GetResponseValidator returns the per-request response validator function from context.
func GetResponseValidator(ctx context.Context) func(resp *http.Response) error {
	cfg := GetRequestConfig(ctx)
	if cfg == nil {
		return nil
	}

	return cfg.ResponseValidator
}

// GetCacheTTL returns the per-request cache TTL duration from context.
func GetCacheTTL(ctx context.Context) generic.Optional[time.Duration] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.CacheTTL <= 0 {
		return generic.None[time.Duration]()
	}

	return generic.Some(cfg.CacheTTL)
}

// Or combines multiple [RetryCondition] predicates, returning true if ANY condition is satisfied.
func Or(conditions ...RetryCondition) RetryCondition {
	return func(resp Response, err error) bool {
		for _, cond := range conditions {
			if cond != nil && cond(resp, err) {
				return true
			}
		}

		return false
	}
}

// And combines multiple [RetryCondition] predicates, returning true if ALL conditions are satisfied.
func And(conditions ...RetryCondition) RetryCondition {
	return func(resp Response, err error) bool {
		for _, cond := range conditions {
			if cond == nil || !cond(resp, err) {
				return false
			}
		}

		return true
	}
}

// FallbackString constructs a [FallbackFunc] returning plain text with the specified status code.
func FallbackString(statusCode int, text string) FallbackFunc {
	return func(req Request, _ error) (Response, error) {
		header := make(http.Header)
		header.Set("Content-Type", "text/plain; charset=utf-8")

		var httpReq *http.Request
		if req != nil {
			httpReq = req.HTTPRequest()
		}

		return NewStdResponse(&http.Response{
			StatusCode:    statusCode,
			Status:        http.StatusText(statusCode),
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        header,
			Body:          stdio.NopCloser(strings.NewReader(text)),
			ContentLength: int64(len(text)),
			Request:       httpReq,
		}), nil
	}
}

// FallbackJSON constructs a [FallbackFunc] returning JSON-encoded data with the specified status code.
func FallbackJSON(statusCode int, data any) FallbackFunc {
	return func(req Request, _ error) (Response, error) {
		bodyBytes, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}

		header := make(http.Header)
		header.Set("Content-Type", "application/json; charset=utf-8")

		var httpReq *http.Request
		if req != nil {
			httpReq = req.HTTPRequest()
		}

		return NewStdResponse(&http.Response{
			StatusCode:    statusCode,
			Status:        http.StatusText(statusCode),
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        header,
			Body:          stdio.NopCloser(bytes.NewReader(bodyBytes)),
			ContentLength: int64(len(bodyBytes)),
			Request:       httpReq,
		}), nil
	}
}

// GetRetryOverride retrieves the per-request [RetryOverride] configuration from context.
func GetRetryOverride(ctx context.Context) generic.Optional[RetryOverride] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.RetryPolicy == nil {
		return generic.None[RetryOverride]()
	}

	if cfg.RetryPolicy != nil {
		return generic.Some(*cfg.RetryPolicy)
	}

	return generic.None[RetryOverride]()
}

// ProxyFuncWithOverride wraps a base proxy resolution function so that per-request proxy overrides take precedence.
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

// TLSConfigWithOverride clones base and applies per-request TLS settings (e.g. InsecureSkipVerify) from context.
func TLSConfigWithOverride(ctx context.Context, base *tls.Config) *tls.Config {
	if !GetInsecureSkipVerify(ctx) {
		return base
	}

	var cloned *tls.Config
	if base != nil {
		cloned = base.Clone()
	} else {
		cloned = &tls.Config{}
	}

	cloned.InsecureSkipVerify = true

	return cloned
}
