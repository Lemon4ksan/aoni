// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	fctx "github.com/lemon4ksan/foundation/async/context"
	flog "github.com/lemon4ksan/foundation/async/log"
	"github.com/lemon4ksan/foundation/generic"
	furl "github.com/lemon4ksan/foundation/net/url"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/experimental"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/netutil/power"
	"github.com/lemon4ksan/aoni/telemetry"
)

// Client is an immutable, thread-safe, multi-protocol HTTP, WebSockets, and gRPC client facade.
// It acts as the primary architectural entry point for high-performance network communications,
// encapsulating complex transport orchestration—including uTLS fingerprinting, HTTP/2-3 framing,
// dynamic proxy rotation, anti-DPI packet fragmentation, and OS-level p0f stack spoofing.
//
// # Architectural Philosophy: Progressive Disclosure of Complexity
//
// The client is designed so that basic HTTP operations require zero cognitive overhead and
// execute along a zero-allocation fast path ("baremetal"). As requirements grow in complexity,
// enterprise-grade capabilities (such as speculative request hedging, automatic WAF challenge solving,
// browser TLS impersonation, and MASQUE/SSH tunneling) can be enabled declaratively via functional
// options without rewriting application logic or breaking existing interfaces.
//
// # Concurrency & Thread-Safety Invariants
//
// Client instances are strictly immutable once initialized and 100% safe for concurrent access
// across arbitrary goroutines. Mutation methods such as [Client.With] and [Client.Clone] return newly
// allocated Client instances with fully isolated configuration DTOs, header maps, and internal state,
// guaranteeing that concurrent operations never suffer from shared-memory data races.
type Client struct {
	// cfg holds the immutable snapshot of all client configuration DTOs (defaults, network, fingerprint, engine).
	cfg Config

	// engine represents the underlying execution target (typically an isolated [*http.Client] or custom [HTTPDoer]).
	engine HTTPDoer

	// pipeline orchestrates the 5-stage middleware chain, interceptors, compression, and WAF challenge solvers.
	pipeline *pipeline.Pipeline[*http.Request, *http.Response]

	// coreEngine maintains shared low-level buffers, header caches, and string builders across requests.
	coreEngine *pipeline.Engine

	// prepared caches precomputed URL prefixes and default header slices to achieve zero-allocation path resolution.
	prepared pipeline.PreparedConfig

	// powerWatcher monitors OS sleep/wake cycles to proactively flush stale TCP keep-alive sockets upon wake-up.
	powerWatcher *power.Watcher

	// referer maintains an atomic navigation state for realistic browser Referer header automation.
	referer *pipeline.RefererState

	// baremetalEligible is a precomputed flag indicating if requests can bypass the pipeline entirely for zero allocations.
	baremetalEligible bool
}

// NewClient instantiates a new thread-safe [Client] wrapping the specified execution target.
//
// Parameters & Defaults:
//   - doer: The underlying execution engine. If nil, defaults to a production-hardened [*http.Client]
//     with a 15-second timeout and 10-hop redirect policy normalized via [DefaultEngine].
//   - opts: Composable functional [ClientOption] layers applied sequentially to configure network,
//     TLS fingerprints, proxy rotators, and pipeline behaviors.
//
// Built-in Defaults:
//   - Response Decompression: Enabled for gzip, brotli, and zstd.
//   - Max Response Body: 10 MB threshold to safeguard against out-of-memory DoS attacks.
//   - Happy Eyeballs v2/v3: 300 ms dual-stack IPv4/IPv6 racing delay (RFC 8305).
//   - User-Agent: Fallback Chrome/Windows User-Agent ensured if none is declared.
//
// Concurrency & Lifecycle:
//
// The returned [Client] is safe for concurrent use across multiple goroutines.
// Background resources (such as power watchers or custom engines) should
// be released via [Client.Close] when the client lifecycle terminates.
func NewClient(doer any, opts ...ClientOption) *Client {
	cfg := Config{
		Defaults: ClientDefaults{
			BaseURL:         &url.URL{},
			Headers:         make(http.Header),
			MaxResponseSize: 10 * 1024 * 1024,
			Pipeline: PipelineConfig{
				Decompress: true,
				Validate:   true,
				Challenge:  true,
			},
		},
		Network: NetworkConfig{
			HappyEyeballsDelay: 300 * time.Millisecond,
		},
	}

	generic.ApplyOptions(&cfg, opts...)

	client := &Client{
		engine:  DefaultEngine(doer),
		referer: &pipeline.RefererState{},
	}
	client.applyConfig(cfg)
	client.ensureUserAgent()

	return client
}

// Clone creates an exact, memory-isolated duplicate of the current [Client] contract.
//
// It is an alias for c.With(), guaranteeing that the returned client is an independent
// contract with zero shared mutable state, preventing cross-goroutine interference.
func (c *Client) Clone() *Client {
	return c.With()
}

// With derives a brand new, fully autonomous [Client] contract with the provided functional options applied.
//
// # Architectural Philosophy: Clients as Immutable Contracts (Not Shared Mutable State)
//
// In traditional net/http ecosystems, [*http.Client] is a mutable container where mutating fields (such as
// Jar, Timeout, or Transport) introduces insidious cross-goroutine data races and temporal coupling.
//
// In aoni, a [Client] is an immutable, value-oriented Execution Contract.
// Invoking With does NOT perform a shallow struct copy and never mutates the receiver. Instead, it:
//  1. Snapshots & Isolates Specification: Deep-copies the configuration DTO ([Config.Clone]).
//  2. Decouples Network Transports: Clones underlying HTTP engines ([CloneHTTPClient]) to isolate socket pools.
//  3. Decouples Stateful Automata: Fork-isolates navigation state ([pipeline.RefererState]) to prevent history leaks.
//  4. Recompiles the Pipeline: Rebuilds precomputed routing tables ([pipeline.PreparedConfig]) and middleware stages.
//
// Invariants & Concurrency Guarantees:
//   - The parent client remains 100% untouched and safe for concurrent execution across other goroutines.
//   - The derived client is a standalone, first-class contract with zero shared mutable references to the parent.
//   - Ideal for deriving tenant-isolated, route-scoped, or authenticated client variations at runtime.
func (c *Client) With(opts ...ClientOption) *Client {
	cfg := c.cfg.Clone()
	generic.ApplyOptions(&cfg, opts...)

	clonedReferer := &pipeline.RefererState{}
	if c.referer != nil {
		clonedReferer.LastURL.Set(c.referer.LastURL.Get())
	}

	clonedEngine := c.engine
	if httpClient, ok := clonedEngine.(*http.Client); ok {
		clonedEngine = CloneHTTPClient(httpClient)
	}

	cloned := &Client{
		engine:  clonedEngine,
		referer: clonedReferer,
	}
	cloned.applyConfig(cfg)

	return cloned
}

// Request executes an HTTP transaction using the given method, path, and optional modifiers,
// returning the raw [*http.Response] stream.
//
// # Path Resolution Rules (RFC 3986)
//   - Relative paths (e.g. "/users", "items/1"): Resolved against the configured BaseURL using
//     precomputed zero-allocation string buffers ([pipeline.PreparedConfig]).
//   - Absolute URLs (e.g. "https://api.example.com/v1"): Executed directly, completely overriding BaseURL.
//
// # Execution Paths: Baremetal vs Pipeline
//   - Baremetal Fast Path: When the client has no active pipeline stages (no interceptors, no hooks,
//     no modifiers, no compression rules), Request executes directly through the engine with 0 heap allocations.
//   - Full Pipeline Path: When modifiers, telemetry, or hooks are present, Request allocates a pooled
//     transaction context and runs through the complete 5-stage middleware pipeline.
//
// # Resource Management Invariant
//
// The caller MUST close the returned response body stream ([http.Response.Body.Close]) when finished
// to prevent socket leaks and allow connection reuse in the keep-alive pool.
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	mods ...RequestModifier,
) (*http.Response, error) {
	// Checked BEFORE any allocation. When the client has no pipeline rules, hooks,
	// modifiers, or per-request config we bypass AcquireTx, NewStdRequest and the
	// full pipeline.Execute and route directly to the underlying engine.
	if c.baremetalEligible && len(mods) == 0 && pipeline.GetRequestConfig(ctx) == nil {
		return c.doBaremetal(ctx, method, path)
	}

	return c.doPipeline(ctx, method, path, mods)
}

// ensureRequestConfig resolves or lazily allocates the per-request transaction container ([pipeline.RequestConfig]).
//
// Lifecycle & Zero-Alloc Invariants:
//  1. Re-entrant Contexts: If ctx already carries a RequestConfig (e.g. from an outer retry loop,
//     middleware, or SDK bridge), it enriches the existing container with client defaults without allocating memory.
//  2. On-Demand Allocation: If the request carries modifiers, default client headers, or advanced network options
//     (proxy, cert pinning, JA4), it acquires a pooled container from sync.Pool ([pipeline.AllocRequestConfig]).
//  3. Minimalist Pass-Through: If no request-level configuration is required, ctx is returned untouched with 0 allocations.
func (c *Client) ensureRequestConfig(ctx context.Context, hasMods bool) context.Context {
	if cfg := pipeline.GetRequestConfig(ctx); cfg != nil {
		c.applyRequestConfigDefaults(cfg)
		return ctx
	}

	if hasMods || len(c.cfg.Defaults.DefaultMods) > 0 || c.needsRequestConfig() {
		var cfg *pipeline.RequestConfig

		ctx, cfg = pipeline.AllocRequestConfig(ctx)
		c.applyRequestConfigDefaults(cfg)
	}

	return ctx
}

func (c *Client) doPipeline(
	ctx context.Context,
	method, path string,
	mods []RequestModifier,
) (*http.Response, error) {
	// Initialize or enrich the per-request transaction context from sync.Pool on demand
	ctx = c.ensureRequestConfig(ctx, len(mods) > 0)

	targetURL, err := c.resolveURL(path)
	if err != nil {
		return nil, err
	}

	// Explicitly initialize HTTP/1.1 protocol constants and default host headers
	// to prevent net/http from performing duplicate string parsing during RoundTrip.
	req := &http.Request{
		Method:     method,
		URL:        targetURL,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     c.applyDefaultHTTPHeader(),
		Body:       http.NoBody,
		Host:       targetURL.Host,
	}

	if ctx != nil {
		req = req.WithContext(ctx)
	}

	// Apply default client-level modifiers first, then per-request modifiers (allowing overrides)
	for _, m := range c.cfg.Defaults.DefaultMods {
		m.ApplyStd(req)
	}

	for _, m := range mods {
		m.ApplyStd(req)
	}

	resp, err := c.execute(req, c.resolvePipeline(req))
	if err != nil {
		return nil, &Error{Op: "request failed", Err: err}
	}

	return resp, nil
}

// Get executes an HTTP GET request against path with optional per-request modifiers.
// Resolves path against BaseURL and returns the raw [*http.Response].
// Caller MUST close resp.Body.
func (c *Client) Get(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodGet, path, mods...)
}

// Post executes an HTTP POST request against path with optional per-request modifiers.
// Modifiers can provide payload bodies via [mod.WithJSONBody], [mod.WithBody], etc.
// Caller MUST close resp.Body.
func (c *Client) Post(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodPost, path, mods...)
}

// Put executes an HTTP PUT request against path with optional per-request modifiers.
// Caller MUST close resp.Body.
func (c *Client) Put(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodPut, path, mods...)
}

// Patch executes an HTTP PATCH request against path with optional per-request modifiers.
// Caller MUST close resp.Body.
func (c *Client) Patch(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodPatch, path, mods...)
}

// Delete executes an HTTP DELETE request against path with optional per-request modifiers.
// Caller MUST close resp.Body.
func (c *Client) Delete(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodDelete, path, mods...)
}

// Head executes an HTTP HEAD request against path to inspect headers without fetching the body.
// Caller MUST close resp.Body.
func (c *Client) Head(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodHead, path, mods...)
}

// doBaremetal executes a request on the minimal allocation path - bypassing AcquireTx,
// NewStdRequest, and the full pipeline. Called only when isBaremetalStaticEligible is true
// and no per-request mods or config are present.
func (c *Client) doBaremetal(ctx context.Context, method, path string) (*http.Response, error) {
	u, err := c.resolveURL(path)
	if err != nil {
		return nil, err
	}

	// nil Header is safe: net/http handles absent request headers correctly.
	// Avoids make(http.Header, 0) allocation on the hot baremetal path.
	req := &http.Request{
		Method:     method,
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     nil,
		Host:       u.Host,
	}

	// Avoid the 2-alloc req.WithContext copy for background/todo contexts with nil Done() channel
	if ctx != nil && ctx.Done() != nil {
		req = req.WithContext(ctx)
	}

	resp, err := c.engine.Do(req)
	if err != nil {
		return nil, &Error{Op: "request failed", Err: err}
	}

	return resp, nil
}

// Do executes a prepared [Request] contract via the client execution pipeline,
// accepting both native aoni.Request and fast.Request adapters.
//
// The caller MUST call resp.Close() on the returned response to release pooled memory.
func (c *Client) Do(req Request) (Response, error) {
	if req == nil {
		return nil, ErrNilRequest
	}

	httpReq, err := c.resolveHTTPRequest(req)
	if err != nil {
		return nil, err
	}

	if httpReq != nil && httpReq.URL != nil {
		cfg := pipeline.GetOrInitRequestConfig(httpReq.Context())
		if cfg.TargetHost == "" && httpReq.URL.Hostname() != "" {
			cfg.TargetHost = httpReq.URL.Hostname()
		}
	}

	resp, err := c.execute(httpReq, c.resolvePipeline(httpReq)) //nolint:bodyclose
	if err != nil {
		return nil, &Error{Op: "request failed", Err: err}
	}

	return NewStdResponse(resp), nil
}

// Close releases background janitor workers and engine resources. Safe for repeated calls.
func (c *Client) Close() {
	if c.coreEngine != nil {
		c.coreEngine.Close()
	}

	if c.powerWatcher != nil {
		c.powerWatcher.Close()
		c.powerWatcher = nil
	}
}

// HTTP returns an [HTTPDoer] adapter executing standard [*http.Request] objects through the pipeline.
func (c *Client) HTTP() HTTPDoer {
	return HTTPDoerFunc(func(req *http.Request) (*http.Response, error) {
		return c.execute(req, c.resolvePipeline(req))
	})
}

func (c *Client) execute(req *http.Request, pipe PipelineConfig) (*http.Response, error) {
	fastCtx := fctx.Wrap(req.Context())
	if req.Context() != fastCtx {
		req = req.WithContext(fastCtx)
	}

	return c.pipeline.Execute(fastCtx, req, c.engine, pipe.toInternal())
}

// Config returns a clone DTO copy of the active client configuration.
func (c *Client) Config() Config {
	return c.cfg.Clone()
}

// Engine yields the underlying, undecorated [HTTPDoer] execution engine.
func (c *Client) Engine() HTTPDoer {
	return c.engine
}

// Defaults retrieves a clone DTO of the client's request defaults.
func (c *Client) Defaults() ClientDefaults {
	return c.cfg.Defaults.Clone()
}

// BaseResponse invokes the configured [BaseResponse] factory function if declared.
func (c *Client) BaseResponse() BaseResponse {
	if c.cfg.Defaults.BaseResponse != nil {
		return c.cfg.Defaults.BaseResponse()
	}

	return nil
}

// Network retrieves a clone DTO of active network transport configurations.
func (c *Client) Network() NetworkConfig {
	return c.cfg.Network.Clone()
}

// Fingerprint retrieves a clone DTO of TLS and HTTP/2 emulation settings.
func (c *Client) Fingerprint() FingerprintConfig {
	return c.cfg.Fingerprint.Clone()
}

// Inspector yields the diagnostic [telemetry.TrafficInspector] if configured.
func (c *Client) Inspector() telemetry.TrafficInspector {
	return c.cfg.Defaults.Inspector
}

// TLSConfig returns a deep copy of the active TLS client configuration.
func (c *Client) TLSConfig() *tls.Config {
	if tr := c.Transport(); tr != nil && tr.TLSClientConfig != nil {
		return tr.TLSClientConfig.Clone()
	}

	return nil
}

// BrowserID inspects active TLS dialers to deduce the active [BrowserID] profile.
func (c *Client) BrowserID() BrowserID {
	if c.cfg.Fingerprint.BrowserID != BrowserNone {
		return c.cfg.Fingerprint.BrowserID
	}

	httpClient, ok := c.engine.(*http.Client)
	if !ok || httpClient.Transport == nil {
		return BrowserNone
	}

	tr, ok := httpClient.Transport.(*http.Transport)
	if ok && tr.DialTLSContext != nil {
		return BrowserChrome
	}

	return BrowserNone
}

// Logger returns the configured diagnostic [core.Logger], or a no-op discard fallback.
func (c *Client) Logger() core.Logger {
	if c.cfg.Defaults.Logger == nil {
		return flog.Discard
	}

	return c.cfg.Defaults.Logger
}

// LogValue implements [slog.LogValuer] for structured logging of client state without allocations.
func (c *Client) LogValue() slog.Value {
	if c == nil {
		return slog.GroupValue()
	}

	attrs := make([]slog.Attr, 0, 4)
	if c.prepared.BaseURL != nil {
		attrs = append(attrs, slog.String("base_url", c.prepared.BaseURL.String()))
	}

	if c.cfg.Fingerprint.BrowserID != BrowserNone {
		attrs = append(attrs, slog.String("browser", c.cfg.Fingerprint.BrowserID.String()))
	}

	if c.cfg.Engine.Timeout > 0 {
		attrs = append(attrs, slog.Duration("timeout", c.cfg.Engine.Timeout))
	}

	return slog.GroupValue(attrs...)
}

// Transport retrieves the underlying [*http.Transport] from the engine.
func (c *Client) Transport() *http.Transport {
	httpClient, ok := c.engine.(*http.Client)
	if !ok || httpClient.Transport == nil {
		return nil
	}

	tr, _ := UnwrapAs[*http.Transport](httpClient.Transport)

	return tr
}

// InitRequestConfig attaches or retrieves a pooled [RequestConfig] on the request context.
func (c *Client) InitRequestConfig(req *http.Request) *http.Request {
	cfg := pipeline.GetRequestConfig(req.Context())
	if cfg == nil {
		var ctx context.Context

		ctx, cfg = pipeline.AllocRequestConfig(req.Context())
		req = req.WithContext(ctx)
	}

	c.applyRequestConfigDefaults(cfg)

	return req
}

// CloseIdleConnections closes all idle keep-alive connections maintained in the pool.
func (c *Client) CloseIdleConnections() {
	if closer, ok := c.engine.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

// needsRequestConfig reports whether active client defaults require attaching a RequestConfig DTO to request contexts.
func (c *Client) needsRequestConfig() bool {
	return c.cfg.Network.SocketController != nil ||
		c.cfg.Fingerprint.TLSClientHelloSpecProvider != nil ||
		len(c.cfg.Fingerprint.CertificatePins) > 0 ||
		c.cfg.Fingerprint.P0fSignature != nil ||
		c.cfg.Fingerprint.JA4Callback != nil ||
		c.cfg.Defaults.QueryEncoder != nil ||
		len(c.cfg.Defaults.Decoders) > 0 ||
		c.cfg.Defaults.MultiReadThreshold > 0 ||
		c.cfg.Network.SSRFGuard ||
		c.cfg.Network.ProxyAddr != nil
}

// computeBaremetalEligible determines if the client configuration permits fast 0-alloc baremetal execution.
func (c *Client) computeBaremetalEligible() bool {
	if len(c.cfg.Defaults.DefaultMods) > 0 {
		return false
	}

	if c.needsRequestConfig() {
		return false
	}

	if c.cfg.Defaults.Inspector != nil || len(c.cfg.Defaults.BeforeRequest) > 0 ||
		len(c.cfg.Defaults.AfterResponse) > 0 ||
		len(c.cfg.Defaults.UARotationProfiles) > 0 {
		return false
	}

	if c.cfg.Defaults.RefererAutomaton || c.cfg.Fingerprint.PacketPadding != nil {
		return false
	}

	pipe := c.cfg.Defaults.Pipeline
	if pipe.Decompress || pipe.Validate || pipe.Challenge || pipe.HAR != nil || pipe.Cache != nil ||
		pipe.Hedging != nil ||
		pipe.DPIJitter != nil {
		return false
	}

	return true
}

// ensureUserAgent guarantees a default User-Agent header is set on client request defaults.
func (c *Client) ensureUserAgent() {
	if c.cfg.Defaults.Headers == nil {
		c.cfg.Defaults.Headers = make(http.Header)
	}

	if c.cfg.Defaults.Headers.Get("User-Agent") == "" {
		c.cfg.Defaults.Headers.Set("User-Agent", DefaultUserAgent)
	}
}

// resolveURL resolves relative path against client BaseURL or parses absolute URL strings.
func (c *Client) resolveURL(path string) (*url.URL, error) {
	if (path == "" || path == "/") && c.prepared.BaseURL != nil {
		clone := *c.prepared.BaseURL
		return &clone, nil
	}

	if len(path) > 0 && path[0] == '/' && c.prepared.BaseURL != nil &&
		(c.prepared.BaseURL.Path == "" || c.prepared.BaseURL.Path == "/") {
		return &url.URL{
			Scheme:   c.prepared.BaseURL.Scheme,
			Host:     c.prepared.BaseURL.Host,
			Path:     path,
			User:     c.prepared.BaseURL.User,
			RawQuery: c.prepared.BaseURL.RawQuery,
		}, nil
	}

	targetURLStr, resolveErr := c.resolveTargetURL(path)
	if resolveErr != nil {
		return nil, resolveErr
	}

	u, parseErr := furl.Parse(targetURLStr)
	if parseErr != nil {
		return nil, &Error{Op: "failed to parse URL", Err: parseErr}
	}

	return u, nil
}

func () {
	len(path) > 0 && path[0] == '/' && c.prepared.BaseURL != nil &&
		(c.prepared.BaseURL.Path == "" || c.prepared.BaseURL.Path == "/")
}

// resolveTargetURL resolves path against BaseURL using precomputed zero-allocation string buffers (engine.PreparedConfig).
// Eliminates url.Parse and string formatting allocations for relative path resolutions on the hot path.
func (c *Client) resolveTargetURL(path string) (string, error) {
	// 1. Fast Path: Absolute HTTP/HTTPS URLs bypass BaseURL resolution entirely with zero allocations
	if len(path) >= 7 && (strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")) {
		return path, nil
	}

	// 2. If no BaseURL is configured, return the path as-is
	if c.cfg.Defaults.BaseURL == nil || c.cfg.Defaults.BaseURL.Host == "" {
		return path, nil
	}

	// 3. Fast Path: Root or empty path returns the precomputed BaseURL string directly (0 allocs)
	if path == "" || path == "/" {
		if c.prepared.BaseURLString != "" {
			return c.prepared.BaseURLString, nil
		}

		return c.cfg.Defaults.BaseURL.String(), nil
	}

	// 4. Hot Path Optimization: Splicing path onto pre-trimmed BaseURL string avoids url.Parse & URL.ResolveReference
	if path[0] == '/' && c.prepared.BaseURLTrimmedString != "" {
		return c.prepared.BaseURLTrimmedString + path, nil
	}

	// 5. Fallback Path: Non-standard or relative subpaths with dot-segments (e.g. "../api") resolve via RFC 3986 reference
	rel, err := furl.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return "", &Error{Op: "invalid path", Err: ErrInvalidPath}
	}

	return c.cfg.Defaults.BaseURL.ResolveReference(rel).String(), nil
}

// resolveHTTPRequest converts a generic [Request] interface into a standard [*http.Request].
// Uses zero-allocation bytesconv string conversions for header mappings.
func (c *Client) resolveHTTPRequest(req Request) (*http.Request, error) {
	if httpReq := req.HTTPRequest(); httpReq != nil {
		return httpReq, nil
	}

	ctx := req.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	var bodyReader io.Reader
	if bs := req.BodyStream(); bs != nil {
		bodyReader = bs
	} else if bb := req.BodyBytes(); len(bb) > 0 {
		bodyReader = bytes.NewReader(bb)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method(), req.URL(), bodyReader)
	if err != nil {
		return nil, &Error{Op: "failed to create http request", Err: err}
	}

	// Zero-copy bridge: If the request originates from fasthttp engine, extract headers
	// using unsafe byte-to-string conversions (bytesconv.B2S) without string heap allocations
	fastAdapter, ok := req.(interface{ FastHTTPRequest() *fasthttp.Request })
	if !ok {
		return httpReq, nil
	}

	fastReq := fastAdapter.FastHTTPRequest()
	if fastReq != nil {
		fastReq.Header.All()(func(k, v []byte) bool {
			httpReq.Header.Add(bytesconv.B2S(k), bytesconv.B2S(v))
			return true
		})

		if host := fastReq.Header.Peek("Host"); len(host) > 0 {
			httpReq.Host = bytesconv.B2S(host)
		}
	}

	return httpReq, nil
}

// applyConfig applies a Config DTO to the client instance, recreating internal engines and transport dialers.
func (c *Client) applyConfig(cfg Config) {
	c.cfg = cfg
	c.coreEngine = pipeline.NewEngine(cfg.Defaults.BaseURL, cfg.Defaults.Headers)
	c.prepared = c.coreEngine.Prepared
	c.baremetalEligible = c.computeBaremetalEligible()

	applyEngineConfig(c, cfg.Engine)
	c.applyDialers(c.Transport())
	c.reapplyH2Settings(c.Transport())
	c.applyPowerManagement(cfg.Network.EnablePowerManagement)

	if len(cfg.Network.CPUAffinityCores) > 0 {
		experimental.ApplyCPUAffinity(cfg.Network.CPUAffinityCores)
	}

	c.pipeline = pipeline.New(
		c.toPipelineDefaults(),
		c.cfg.Fingerprint.ToPipelineFingerprint(),
	)
}

// applyPowerManagement manages the lifecycle of OS power suspend/resume watchers.
// When laptops sleep and wake, existing TCP keep-alives silently rot. Flushing the pool
// on wake-up prevents insidious "connection reset by peer" or 15s write timeout stalls.
func (c *Client) applyPowerManagement(enable bool) {
	if !enable {
		if c.powerWatcher != nil {
			c.powerWatcher.Close()
			c.powerWatcher = nil
		}

		return
	}

	if c.powerWatcher == nil {
		watcher := power.NewWatcher(5 * time.Second)
		watcher.OnSuspend(func() {
			c.CloseIdleConnections()
		})

		c.powerWatcher = watcher
	}
}

// applyDefaultHTTPHeader applies the default and precomputed HTTP headers to the request.
// Allocates the header map with precise exact capacity upfront to prevent intermediate bucket rehashing.
func (c *Client) applyDefaultHTTPHeader() http.Header {
	headerCap := len(c.prepared.PrecomputedDefaultHeaders) + len(c.cfg.Defaults.Headers)
	if headerCap == 0 {
		return nil
	}

	reqHeader := make(http.Header, headerCap)
	if len(c.prepared.PrecomputedDefaultHeaders) > 0 {
		for i := range c.prepared.PrecomputedDefaultHeaders {
			h := &c.prepared.PrecomputedDefaultHeaders[i]
			reqHeader[h.Key] = h.Slice
		}
	}

	if len(c.cfg.Defaults.Headers) > 0 {
		for k, v := range c.cfg.Defaults.Headers {
			reqHeader[k] = slices.Clone(v)
		}
	}

	return reqHeader
}

// Unwrap returns the underlying execution engine or inner requester.
func (c *Client) Unwrap() any {
	if c == nil {
		return nil
	}

	if rh, ok := c.engine.(requesterHTTPDoer); ok {
		return rh.r
	}

	return c.engine
}

var (
	_ RequestDoer           = (*Client)(nil)
	_ Configurable[*Client] = (*Client)(nil)
)
