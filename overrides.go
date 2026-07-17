// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package aoni – per-request transport overrides.
//
// Every modifier in this file stores its value in the request context
// using an unexported key type (the "Context Accessors" pattern). The
// transport layer reads the value back with the matching Get* function.
// This avoids creating a new [Client] for each one-off tweak.

package aoni

import (
	"context"
	"crypto/tls"
	"math/rand/v2"
	"net/http"
	"net/url"
	"time"

	"github.com/lemon4ksan/miyako/generic"
)

// MarkModifierError attaches an error to the request config.
// The [Client] will check for this error before dispatching the request
// and return it if present, preventing malformed requests from being sent.
func MarkModifierError(req *http.Request, err error) {
	if err == nil {
		return
	}

	getOrInitRequestConfig(req).BodyError = err
}

// WithProxyOverride returns a [RequestModifier] that routes this single request
// through proxyURL, ignoring the client-level proxy setting.
//
// Example:
//
//	resp, err := client.Get(ctx, "/api", aoni.WithProxyOverride("http://10.0.0.1:8080"))
func WithProxyOverride(rawURL string) RequestModifier {
	return func(req *http.Request) {
		if u, err := url.Parse(rawURL); err == nil {
			getOrInitRequestConfig(req).ProxyAddr = u
		}
	}
}

// GetProxyOverride returns the per-request proxy URL stored by [WithProxyOverride].
func GetProxyOverride(ctx context.Context) generic.Optional[string] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.ProxyAddr == nil {
		return generic.None[string]()
	}

	return generic.Some(cfg.ProxyAddr.String())
}

// WithInsecureSkipVerify returns a [RequestModifier] that disables TLS certificate
// verification for this single request. Useful for requests to internal admin panels
// or self-signed proxies without lowering the security of the entire client.
//
// Only takes effect when the underlying transport honours [GetInsecureSkipVerify].
// The built-in [Client] wires this automatically for uTLS connections (see
// [Client.WithTLSFingerprint]) and plain [http.Transport] connections.
//
//nolint:gosec
func WithInsecureSkipVerify() RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).InsecureSkipVerify = true
	}
}

// GetInsecureSkipVerify reports whether the request carries a per-request
// InsecureSkipVerify flag set by [WithInsecureSkipVerify].
func GetInsecureSkipVerify(ctx context.Context) bool {
	cfg := GetRequestConfig(ctx)
	return cfg != nil && cfg.InsecureSkipVerify
}

// TCPDelayRange describes a random jitter window applied before the TCP dial.
type TCPDelayRange struct {
	Min time.Duration
	Max time.Duration
}

// WithTCPDelay returns a [RequestModifier] that introduces a random delay between
// min and max before the TCP connection is established. This simulates the timing
// characteristics of a real human connection and can confuse bot-detection heuristics
// that look for suspiciously uniform request cadences.
//
// The delay is enforced by [GetTCPDelay] inside the transport dialer.
// If min > max the values are swapped silently.
func WithTCPDelay(min, max time.Duration) RequestModifier {
	if min > max {
		min, max = max, min
	}

	return func(req *http.Request) {
		getOrInitRequestConfig(req).TCPDelay = TCPDelayRange{Min: min, Max: max}
	}
}

// GetTCPDelay returns the [TCPDelayRange] stored by [WithTCPDelay] and
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

// WithConnMetadata attaches an arbitrary key-value pair to the request config.
// Useful for propagating metadata (e.g. proxy IDs, pool tags) into transport hooks,
// [TrafficInspector] captures, or structured log output.
//
// Multiple calls with different keys are cumulative. Duplicate keys overwrite the
// previous value, matching standard context semantics.
func WithConnMetadata(key string, val any) RequestModifier {
	return func(req *http.Request) {
		cfg := getOrInitRequestConfig(req)
		if cfg.Metadata == nil {
			cfg.Metadata = make(map[string]any)
		}

		cfg.Metadata[key] = val
	}
}

// GetConnMetadata retrieves a value previously set by [WithConnMetadata].
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

// WithResponseValidator attaches a validation function that is invoked by
// [Client.Request] immediately after a successful HTTP round-trip, before the
// response body is decoded. If fn returns a non-nil error the client treats
// the request as failed and propagates the error to the caller.
//
// This is the recommended way to implement anti-bot body inspection (e.g.
// looking for "Access Denied" strings) without writing a full middleware:
//
//	WithResponseValidator(func(resp *http.Response) error {
//	    if resp.Header.Get("X-Shield") == "blocked" {
//	        return errors.New("request was shielded")
//	    }
//	    return nil
//	})
//
// The validator receives the raw *http.Response before any decompression or
// transcoding has been applied by the client. Do not close resp.Body inside fn.
func WithResponseValidator(fn func(resp *http.Response) error) RequestModifier { //nolint:bodyclose
	return func(req *http.Request) {
		cfg := getOrInitRequestConfig(req)

		existing := cfg.ResponseValidator
		if existing != nil {
			cfg.ResponseValidator = func(resp *http.Response) error {
				err1 := existing(resp)

				err2 := fn(resp)
				if err2 != nil {
					return err2
				}

				return err1
			}
		} else {
			cfg.ResponseValidator = fn
		}
	}
}

// GetResponseValidator returns the per-request validator registered by
// [WithResponseValidator], or nil when none is set.
func GetResponseValidator(ctx context.Context) func(resp *http.Response) error {
	cfg := GetRequestConfig(ctx)
	if cfg == nil {
		return nil
	}

	return cfg.ResponseValidator
}

// WithCacheTTL marks the request as cacheable with the given TTL.
// A caching [Middleware] (e.g. built with [CacheMiddleware]) should call
// [GetCacheTTL] to decide whether and how long to cache the response.
// A zero or negative duration means "do not cache".
func WithCacheTTL(d time.Duration) RequestModifier {
	return func(req *http.Request) {
		getOrInitRequestConfig(req).CacheTTL = d
	}
}

// GetCacheTTL returns the per-request cache TTL set by [WithCacheTTL].
func GetCacheTTL(ctx context.Context) generic.Optional[time.Duration] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.CacheTTL <= 0 {
		return generic.None[time.Duration]()
	}

	return generic.Some(cfg.CacheTTL)
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

// WithRetryPolicy returns a [RequestModifier] that overrides the global retry
// settings for this request. A [RetryMiddleware] should call [GetRetryOverride]
// and prefer the per-request values when present.
//
// Example - disable retries for a critical mutating action:
//
//	client.Post(ctx, "/checkout", body, aoni.WithRetryPolicy(aoni.RetryOverride{MaxAttempts: 1}))
//
// Example - aggressive retry for a read-only scrape:
//
//	client.Get(ctx, "/prices", aoni.WithRetryPolicy(aoni.RetryOverride{
//	    MaxAttempts: 10,
//	    Backoff:     500 * time.Millisecond,
//	    Condition:   aoni.Or(aoni.RetryOnErr(), aoni.RetryOnGatewayErrors()),
//	}))
func WithRetryPolicy(o RetryOverride) RequestModifier {
	if o.MaxAttempts < 1 {
		o.MaxAttempts = 1
	}

	if o.Condition == nil {
		o.Condition = RetryOnErr()
	}

	return func(req *http.Request) {
		getOrInitRequestConfig(req).RetryPolicy = &o
	}
}

// GetRetryOverride returns the per-request [RetryOverride] set by [WithRetryPolicy].
func GetRetryOverride(ctx context.Context) generic.Optional[RetryOverride] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.RetryPolicy == nil {
		return generic.None[RetryOverride]()
	}

	return generic.Some(*cfg.RetryPolicy)
}

// ProxyFuncWithOverride wraps a base proxy function (e.g. [http.ProxyURL] or
// [http.ProxyFromEnvironment]) so that a per-request [WithProxyOverride] value
// takes precedence. Pass the result as [http.Transport.Proxy].
//
//	transport.Proxy = aoni.ProxyFuncWithOverride(http.ProxyFromEnvironment)
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
// found in ctx (currently [WithInsecureSkipVerify]). Returns base unchanged
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
