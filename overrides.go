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
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/silicon/pool"
	fastrand "github.com/lemon4ksan/foundation/silicon/rand"

	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/telemetry"
)

// AsReplayable wraps an io.ReadCloser into a replayable stream ([io.ReplayableBody])
// using in-memory byte buffers or tee-buffered fallbacks to support stream rewinding.
var AsReplayable = io.AsReplayable

// ResponseTrace extracts fine-grained execution metrics and network timing details
// ([telemetry.TraceInfo]) captured during request execution from the response context.
// Returns nil if no trace container was registered on the request.
// Safe for concurrent access.
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

// HostRewriteRules extracts per-request hostname-to-IP/host remapping rules from the context.
// Returns nil if no host rewrite rules are attached to the request context.
func HostRewriteRules(ctx context.Context) map[string]string {
	cfg := GetRequestConfig(ctx)
	if cfg != nil && cfg.HostRewrite != nil {
		return cfg.HostRewrite.Rules
	}

	return nil
}

// WithContextModifier attaches functional [RequestModifier] closures directly to a context.
// Third-party HTTP SDKs (e.g. Resty, AWS SDK, Azure SDK) carrying this context will
// automatically propagate these modifiers into the aoni execution pipeline.
// Modifiers are attached using thread-safe slice copying to prevent data races.
func WithContextModifier(ctx context.Context, mods ...RequestModifier) context.Context {
	if len(mods) == 0 {
		return ctx
	}

	cfg := GetRequestConfig(ctx)
	if cfg == nil {
		ctx, cfg = AllocRequestConfig(ctx)
	}

	modsCopy := make([]pipeline.RequestModifier, 0, len(cfg.Modifiers)+len(mods))
	modsCopy = append(modsCopy, cfg.Modifiers...)
	modsCopy = append(modsCopy, mods...)
	cfg.Modifiers = modsCopy

	return ctx
}

// ContextModifiers retrieves all per-request [RequestModifier] closures stored in the context.
func ContextModifiers(ctx context.Context) []pipeline.RequestModifier {
	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		return cfg.Modifiers
	}

	return nil
}

// MarkModifierError attaches a serialization or setup error to the request context.
// When an error is present, the client pipeline aborts execution prior to transmitting data on the wire.
func MarkModifierError(req any, err error) {
	if err == nil {
		return
	}

	GetOrInitRequestConfig(req).BodyError = err
}

// GetProxyOverride retrieves the per-request proxy server URL stored in the context.
// Returns generic.None if no proxy override is set for the current execution.
func GetProxyOverride(ctx context.Context) generic.Optional[string] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.ProxyAddr == nil {
		return generic.None[string]()
	}

	return generic.Some(cfg.ProxyAddr.String())
}

// GetInsecureSkipVerify reports whether TLS certificate and hostname verification is bypassed
// for the current request execution context.
func GetInsecureSkipVerify(ctx context.Context) bool {
	cfg := GetRequestConfig(ctx)
	return cfg != nil && cfg.InsecureSkipVerify
}

// TCPDelayRange defines minimum and maximum bounds for randomized pre-dial TCP delay jitter.
type TCPDelayRange = pipeline.TCPDelayRange

// GetTCPDelay retrieves the configured pre-dial TCP delay jitter range from context.
func GetTCPDelay(ctx context.Context) generic.Optional[TCPDelayRange] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.TCPDelay.Max <= 0 {
		return generic.None[TCPDelayRange]()
	}

	return generic.Some(cfg.TCPDelay)
}

// ApplyTCPDelay inspects the context for pre-dial TCP delay jitter settings and pauses
// execution for a randomized duration within those bounds prior to opening L4 sockets.
// Safe for concurrent use across multiple goroutines.
func ApplyTCPDelay(ctx context.Context) error {
	r, ok := GetTCPDelay(ctx).Value()
	if !ok || r.Max <= 0 {
		return nil
	}

	window := r.Max - r.Min

	delay := r.Min
	if window > 0 {
		delay += fastrand.FastJitter(window)
	}

	if delay <= 0 {
		return nil
	}

	t := pool.AcquireTimer(delay)
	select {
	case <-t.C:
		pool.ReleaseTimer(t)
		return nil
	case <-ctx.Done():
		pool.ReleaseTimer(t)
		return ctx.Err()
	}
}

// GetConnMetadata retrieves custom connection metadata stored under key in the request context.
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

// GetResponseValidator retrieves the per-request response validation callback from context.
func GetResponseValidator(ctx context.Context) func(resp *http.Response) error {
	cfg := GetRequestConfig(ctx)
	if cfg == nil {
		return nil
	}

	return cfg.ResponseValidator
}

// GetCacheTTL retrieves the per-request HTTP response caching TTL duration from context.
func GetCacheTTL(ctx context.Context) generic.Optional[time.Duration] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.CacheTTL <= 0 {
		return generic.None[time.Duration]()
	}

	return generic.Some(cfg.CacheTTL)
}

// GetTimeoutOverride retrieves the per-request timeout duration override from context.
func GetTimeoutOverride(ctx context.Context) generic.Optional[time.Duration] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.TimeoutOverride <= 0 {
		return generic.None[time.Duration]()
	}

	return generic.Some(cfg.TimeoutOverride)
}

// GetDNSResolverOverride retrieves the per-request DNS resolver override from context.
func GetDNSResolverOverride(ctx context.Context) netdial.DNSResolver {
	cfg := GetRequestConfig(ctx)
	if cfg != nil {
		return cfg.DNSResolver
	}

	return nil
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

func newSyntheticResponse(
	statusCode int,
	contentType string,
	bodyReader stdio.Reader,
	contentLength int64,
	req Request,
) Response {
	header := make(http.Header)
	header.Set("Content-Type", contentType)

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
		Body:          stdio.NopCloser(bodyReader),
		ContentLength: contentLength,
		Request:       httpReq,
	})
}

// FallbackString constructs a synthetic [FallbackFunc] returning plain text with the specified status code.
func FallbackString(statusCode int, text string) FallbackFunc {
	return func(req Request, _ error) (Response, error) {
		return newSyntheticResponse(
			statusCode,
			"text/plain; charset=utf-8",
			strings.NewReader(text),
			int64(len(text)),
			req,
		), nil
	}
}

// FallbackJSON constructs a synthetic [FallbackFunc] returning JSON-encoded data with the specified status code.
func FallbackJSON(statusCode int, data any) FallbackFunc {
	return func(req Request, _ error) (Response, error) {
		bodyBytes, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}

		return newSyntheticResponse(
			statusCode,
			"application/json; charset=utf-8",
			bytes.NewReader(bodyBytes),
			int64(len(bodyBytes)),
			req,
		), nil
	}
}

// GetRetryOverride retrieves the per-request [RetryOverride] configuration from context.
func GetRetryOverride(ctx context.Context) generic.Optional[RetryOverride] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.RetryPolicy == nil {
		return generic.None[RetryOverride]()
	}

	return generic.Some(*cfg.RetryPolicy)
}

// ProxyFuncWithOverride wraps a base proxy resolution function so that per-request
// proxy overrides attached to the context take precedence.
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

// TLSConfigWithOverride clones base and applies per-request TLS settings
// (such as InsecureSkipVerify) extracted from the request context.
func TLSConfigWithOverride(ctx context.Context, base *tls.Config) *tls.Config {
	return pipeline.TLSConfigWithOverride(base, GetInsecureSkipVerify(ctx))
}

// ApplyCPUAffinity locks the calling goroutine's OS thread to designated physical CPU cores.
func ApplyCPUAffinity(cores []int) {
	pipeline.ApplyCPUAffinity(cores)
}
