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
