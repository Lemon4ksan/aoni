// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	furl "github.com/lemon4ksan/foundation/net/url"
	"github.com/lemon4ksan/foundation/silicon/pool"
	frand "github.com/lemon4ksan/foundation/silicon/rand"

	"github.com/lemon4ksan/aoni/internal/core"
	aio "github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/netdial"
	"github.com/lemon4ksan/aoni/telemetry"
)

// AsReplayable wraps an [io.ReadCloser] into a replayable stream ([aio.ReplayableBody])
// using in-memory byte buffers or tee-buffered fallbacks to support stream rewinding.
func AsReplayable(rc io.ReadCloser) aio.ReplayableBody {
	return aio.AsReplayable(rc)
}

// ResponseTrace extracts fine-grained execution metrics and network timing details
// ([telemetry.TraceInfo]) captured during request execution from the response context.
// Returns nil if no trace container was registered on the request.
// Safe for concurrent access.
func ResponseTrace(resp *http.Response) *telemetry.TraceInfo {
	if resp == nil || resp.Request == nil {
		return nil
	}

	if cfg := GetRequestConfig(resp.Request.Context()); cfg != nil {
		return cfg.TraceInfo
	}

	return nil
}

// HostRewriteRules extracts per-request hostname-to-IP/host remapping rules from the context.
// Returns nil if no host rewrite rules are attached to the request context.
func HostRewriteRules(ctx context.Context) map[string]string {
	if cfg := GetRequestConfig(ctx); cfg != nil && cfg.HostRewrite != nil {
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
		ctx, cfg = pipeline.AllocRequestConfig(ctx)
	}

	modsCopy := make([]RequestModifier, 0, len(cfg.Modifiers)+len(mods))
	modsCopy = append(modsCopy, cfg.Modifiers...)
	modsCopy = append(modsCopy, mods...)
	cfg.Modifiers = modsCopy

	return ctx
}

// ContextModifiers retrieves all per-request [RequestModifier] closures stored in the context.
func ContextModifiers(ctx context.Context) []RequestModifier {
	if cfg := GetRequestConfig(ctx); cfg != nil {
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

	pipeline.GetOrInitRequestConfig(req).BodyError = err
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
type TCPDelayRange = netutil.TCPDelayRange

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
		delay += frand.Jitter(window)
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
	if cfg := GetRequestConfig(ctx); cfg != nil {
		return cfg.ResponseValidator
	}

	return nil
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
	if cfg := GetRequestConfig(ctx); cfg != nil {
		return cfg.DNSResolver
	}

	return nil
}

// GetRetryOverride retrieves the per-request [core.RetryOverride] configuration from context.
func GetRetryOverride(ctx context.Context) generic.Optional[core.RetryOverride] {
	cfg := GetRequestConfig(ctx)
	if cfg == nil || cfg.RetryPolicy == nil {
		return generic.None[core.RetryOverride]()
	}

	return generic.Some(*cfg.RetryPolicy)
}

// ProxyFuncWithOverride wraps a base proxy resolution function so that per-request
// proxy overrides attached to the context take precedence.
func ProxyFuncWithOverride(base func(*http.Request) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		if raw, ok := GetProxyOverride(req.Context()).Value(); ok && raw != "" {
			return furl.Parse(raw)
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
