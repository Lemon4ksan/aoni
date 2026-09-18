// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/lemon4ksan/foundation/async/logkit"
	"github.com/lemon4ksan/foundation/borrow"
	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/net/urlkit"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/x/telemetry"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/netutil/dict"
	"github.com/lemon4ksan/aoni/netutil/power"
	"github.com/lemon4ksan/aoni/pipeline"
)

// Client is a thread-safe HTTP client supporting custom transports, TLS fingerprinting,
// and request/response middleware pipelines.
//
// Concurrency:
// All methods on Client are safe for concurrent use by multiple goroutines.
// Client instances are immutable after construction; derivation methods such as
// [Client.With] and [Client.Clone] return a new independent Client instance.
type Client struct {
	// cfg holds the immutable snapshot of all client configuration DTOs (defaults, network, fingerprint, engine).
	cfg Config

	// engine represents the underlying execution target (typically an isolated [*http.Client] or custom [HTTPDoer]).
	engine HTTPDoer

	// pipeline orchestrates the 5-stage middleware chain, interceptors, compression, and WAF challenge solvers.
	pipeline *pipeline.Pipeline[*http.Request, *http.Response]

	// coreEngine maintains shared low-level buffers, header caches, and string builders across requests.
	coreEngine *pipeline.Engine

	// prepared caches precomputed URL prefixes and default header slices to achieve path resolution.
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
	if opt, ok := doer.(ClientOption); ok { // seamless transition from fast client
		opts = append([]ClientOption{opt}, opts...)
		doer = nil
	}

	cfg := Config{
		Defaults: ClientDefaults{
			BaseURL:         &url.URL{},
			Headers:         make(http.Header),
			MaxResponseSize: 10 * 1024 * 1024,
			DictionaryStore: dict.NewStore(),
			Pipeline: PipelineConfig{
				Decompress: true,
				Validate:   true,
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

// Unwrap returns the underlying execution engine.
func (c *Client) Unwrap() HTTPDoer {
	return c.engine
}

// Clone creates an independent copy of the Client with isolated configuration state.
// It is an alias for c.With().
func (c *Client) Clone() *Client {
	return c.With()
}

// With returns a new Client with the provided options applied.
//
// Concurrency:
// The receiver Client is not modified and remains safe for concurrent use.
// The returned Client is fully independent with isolated configuration state.
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

// Request executes an HTTP request using the specified method, path, and modifiers,
// returning the raw [*http.Response].
//
// Path resolution:
//   - Relative paths (e.g. "/users"): Resolved against the configured BaseURL.
//   - Absolute URLs (e.g. "https://api.example.com"): Executed directly, overriding BaseURL.
//
// Resource management:
// The caller is responsible for closing the response body (resp.Body.Close()).
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
func (c *Client) ensureRequestConfig(ctx context.Context, hasMods bool) context.Context {
	ctx = generic.Coalesce(ctx, context.Background())

	if cfg := pipeline.GetRequestConfig(ctx); cfg != nil {
		c.applyRequestConfigDefaults(cfg)
		return ctx
	}

	if hasMods || len(c.cfg.Defaults.DefaultMods) > 0 || c.cfg.RequiresRequestContext() {
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

	if cfg := pipeline.GetRequestConfig(req); cfg != nil && cfg.BodyError != nil {
		return nil, cfg.BodyError
	}

	resp, err := c.execute(req, c.resolvePipeline(req))
	if err != nil {
		return nil, &Error{Op: "execute", Err: err}
	}

	return resp, nil
}

// DoScoped executes an aoni Request within an auto-releasing [borrow.Scope] context.
// The response stream is automatically closed upon callback completion,
// guaranteeing zero memory leaks and safe buffer recycling.
func (c *Client) DoScoped(
	req Request,
	fn func(s *borrow.Scope, resp Response) error,
) error {
	resp, err := c.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		_ = resp.Close()
	}()

	scope := borrow.AcquireScope()
	defer scope.Release()

	return fn(scope, resp)
}

// RequestScoped executes an HTTP request within an auto-releasing [borrow.Scope] context.
func (c *Client) RequestScoped(
	ctx context.Context,
	method, path string,
	fn func(s *borrow.Scope, resp *http.Response) error,
	mods ...RequestModifier,
) error {
	resp, err := c.Request(ctx, method, path, mods...)
	if err != nil {
		return err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	scope := borrow.AcquireScope()
	defer scope.Release()

	return fn(scope, resp)
}

// Head executes an HTTP HEAD request against path to inspect headers without fetching the body.
// Caller MUST close resp.Body.
func (c *Client) Head(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodHead, path, mods...)
}

// Get executes a raw HTTP GET request against path and returns the raw [*http.Response].
//
// # Resource Management
//
// Caller MUST close resp.Body to prevent socket leaks.
func (c *Client) Get(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodGet, path, mods...)
}

// Post executes a raw HTTP POST request carrying body and returns the raw [*http.Response].
//
// The body argument is automatically detected and serialized:
//   - Struct / Map / Slice -> JSON payload with "Content-Type: application/json"
//   - [proto.Message] -> Protobuf binary payload with "Content-Type: application/x-protobuf"
//   - [url.Values] -> Form payload with "Content-Type: application/x-www-form-urlencoded"
//   - `[]byte` / `string` / [io.Reader] -> Raw payload (no default Content-Type header)
func (c *Client) Post(ctx context.Context, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	return c.Fetch(ctx, http.MethodPost, path, body, mods...)
}

// Put executes a raw HTTP PUT request carrying body and returns the raw [*http.Response].
//
// See [Client.Post] for automatic body detection and serialization rules.
func (c *Client) Put(ctx context.Context, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	return c.Fetch(ctx, http.MethodPut, path, body, mods...)
}

// Patch executes a raw HTTP PATCH request carrying body and returns the raw [*http.Response].
//
// See [Client.Post] for automatic body detection and serialization rules.
func (c *Client) Patch(ctx context.Context, path string, body any, mods ...RequestModifier) (*http.Response, error) {
	return c.Fetch(ctx, http.MethodPatch, path, body, mods...)
}

// Delete executes a raw HTTP DELETE request and returns the raw [*http.Response].
//
// # Resource Management
//
// Caller MUST close resp.Body to prevent socket leaks.
func (c *Client) Delete(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodDelete, path, mods...)
}

// Options executes a raw HTTP OPTIONS request and returns the raw [*http.Response].
//
// # Resource Management
//
// Caller MUST close resp.Body to prevent socket leaks.
func (c *Client) Options(ctx context.Context, path string, mods ...RequestModifier) (*http.Response, error) {
	return c.Request(ctx, http.MethodOptions, path, mods...)
}

// Fetch executes an arbitrary raw HTTP method request and returns the raw [*http.Response].
//
// See [Client.Post] for automatic body detection and serialization rules.
func (c *Client) Fetch(
	ctx context.Context,
	method, path string,
	body any,
	mods ...RequestModifier,
) (*http.Response, error) {
	var stackBuf [stackModCap]RequestModifier

	allMods, err := prepareBodyMods(body, mods, &stackBuf)
	if err != nil {
		return nil, err
	}

	return c.Request(ctx, method, path, allMods...)
}

// doBaremetal executes a request directly through the underlying engine,
// bypassing transaction context allocation and the pipeline.
// Invoked when no modifiers or pipeline features are active.
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

	// Avoid the 2-alloc req.WithContext copy for the two sentinel background contexts.
	// Checking ctx.Done() == nil is insufficient: context.WithValue(context.Background(), k, v)
	// also has a nil Done channel but carries values (trace IDs, auth tokens, etc.) that must
	// not be silently dropped. Identity comparison against the two stdlib singletons is exact.
	if ctx != nil && ctx != context.Background() && ctx != context.TODO() {
		req = req.WithContext(ctx)
	}

	resp, err := c.engine.Do(req)
	if err != nil {
		return nil, &Error{Op: "execute", Err: err}
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

	httpReq, err := ToStdRequest(req)
	if err != nil {
		return nil, err
	}

	if httpReq != nil && httpReq.URL != nil {
		cfg := pipeline.GetOrInitRequestConfig(req)
		if cfg.BodyError != nil {
			return nil, cfg.BodyError
		}

		if cfg.TargetHost == "" && httpReq.URL.Hostname() != "" {
			cfg.TargetHost = httpReq.URL.Hostname()
		}
	}

	if cfg := pipeline.GetOrInitRequestConfig(req); cfg != nil {
		httpReq = httpReq.WithContext(context.WithValue(httpReq.Context(), pipeline.RequestConfigKey{}, cfg))
	}

	resp, err := c.execute(httpReq, c.resolvePipeline(httpReq)) //nolint:bodyclose
	if err != nil {
		return nil, &Error{Op: "execute", Err: err}
	}

	return NewStdResponse(resp), nil
}

// Close releases background janitor workers and engine resources. Safe for repeated calls.
func (c *Client) Close() {
	c.CloseIdleConnections()

	if closer, ok := c.engine.(io.Closer); ok {
		_ = closer.Close()
	}

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
	return c.pipeline.Execute(req.Context(), req, c.engine, pipe.toInternal())
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
	if c == nil || c.cfg.Defaults.BaseResponse == nil {
		return nil
	}

	return c.cfg.Defaults.BaseResponse()
}

// Network retrieves a clone DTO of active network transport configurations.
func (c *Client) Network() NetworkConfig {
	return c.cfg.Network.Clone()
}

// Jar returns the active [http.CookieJar] configured on the client, or nil if none is set.
func (c *Client) Jar() http.CookieJar {
	return c.cfg.Engine.CookieJar
}

// Cookies retrieves cookies from the active jar matching destination u.
func (c *Client) Cookies(u *url.URL) []*http.Cookie {
	jar := c.cfg.Engine.CookieJar
	if jar == nil || u == nil {
		return nil
	}

	return jar.Cookies(u)
}

// SetCookies injects cookies into the active cookie jar bound to destination u.
func (c *Client) SetCookies(u *url.URL, cookies []*http.Cookie) {
	jar := c.cfg.Engine.CookieJar
	if jar != nil && u != nil && len(cookies) > 0 {
		jar.SetCookies(u, cookies)
	}
}

// HasCookies reports whether the client cookie jar holds any active cookies for URL u.
func (c *Client) HasCookies(u *url.URL) bool {
	jar := c.cfg.Engine.CookieJar
	if jar == nil || u == nil {
		return false
	}

	return len(jar.Cookies(u)) > 0
}

// FindCookie searches for a cookie by name for a given URL and reports whether it was found.
func (c *Client) FindCookie(u *url.URL, name string) (*http.Cookie, bool) {
	jar := c.cfg.Engine.CookieJar
	if jar == nil || u == nil {
		return nil, false
	}

	if finder, ok := jar.(cookie.Finder); ok {
		return finder.FindCookie(u, name)
	}

	return generic.Find(jar.Cookies(u), func(ck *http.Cookie) bool {
		return ck != nil && ck.Name == name
	})
}

// GetCookieValue retrieves the value of a named cookie.
func (c *Client) GetCookieValue(u *url.URL, name string) (string, bool) {
	if ck, ok := c.FindCookie(u, name); ok && ck != nil {
		return ck.Value, true
	}

	return "", false
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

// Logger returns the configured diagnostic [core.Logger], or a no-op discard fallback.
func (c *Client) Logger() core.Logger {
	if c.cfg.Defaults.Logger == nil {
		return logkit.Discard
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

	if c.cfg.Engine.Timeout > 0 {
		attrs = append(attrs, slog.Duration("timeout", c.cfg.Engine.Timeout))
	}

	return slog.GroupValue(attrs...)
}

// Transport retrieves the underlying [*http.Transport] from the engine.
func (c *Client) Transport() *http.Transport {
	if c == nil || c.engine == nil {
		return nil
	}

	if httpClient, ok := UnwrapAs[*http.Client](c.engine); ok && httpClient.Transport != nil {
		tr, _ := UnwrapAs[*http.Transport](httpClient.Transport)
		return tr
	}

	if tp, ok := UnwrapAs[interface{ Transport() *http.Transport }](c.engine); ok {
		return tp.Transport()
	}

	return nil
}

// InitRequestConfig attaches or retrieves a pooled [RequestConfig] on the request context.
func (c *Client) InitRequestConfig(req *http.Request) *http.Request {
	cfg := pipeline.GetRequestConfig(req)
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
	if closer, ok := UnwrapAs[interface{ CloseIdleConnections() }](c.engine); ok {
		closer.CloseIdleConnections()
	}
}

// Preresolve proactively executes DNS resolution for the given host and warms the internal DNS cache.
// Useful for prefetching domain resolutions in latency-critical workflows.
func (c *Client) Preresolve(ctx context.Context, host string) error {
	if host == "" {
		return nil
	}

	if c.cfg.Network.DNSResolver != nil {
		_, err := c.cfg.Network.DNSResolver.LookupIPAddr(ctx, host)
		return err
	}

	_, err := net.DefaultResolver.LookupIPAddr(ctx, host)

	return err
}

// Preconnect proactively warms the transport connection pool for targetURL by
// performing a zero-body HEAD request. This drives a full DNS lookup, TCP dial,
// and TLS/ALPN handshake, and the resulting connection is retained in the pool
// for immediate reuse by subsequent requests, eliminating first-byte latency.
//
// Note: net/http does not expose a way to warm its pool without sending an actual
// HTTP request; a HEAD with no body is the lowest-overhead mechanism available.
//
// Error handling:
//   - Transport-level errors (DNS failure, TCP refused, TLS mismatch) are returned
//     so callers can detect that the host is unreachable before committing work.
//   - HTTP-level errors (4xx/5xx, e.g. 405 Method Not Allowed) are silently ignored:
//     the TCP+TLS round-trip succeeded and the pool is already warmed.
func (c *Client) Preconnect(ctx context.Context, targetURL string) error {
	u, err := c.resolveURL(targetURL)
	if err != nil {
		return err
	}

	resp, err := c.Request(ctx, http.MethodHead, u.String())
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	if err == nil {
		return nil
	}

	// HTTP-level error (4xx/5xx): TCP+TLS succeeded, connection pool is warmed.
	if _, ok := errors.AsType[*APIError](err); ok {
		return nil
	}

	// Transport-level error: DNS, TCP dial, or TLS handshake failed — real failure.
	return err
}

// ensureUserAgent guarantees a default User-Agent header is set on client request defaults.
func (c *Client) ensureUserAgent() {
	if c.cfg.Defaults.Headers == nil {
		c.cfg.Defaults.Headers = make(http.Header)
	}

	if c.cfg.Defaults.Headers.Get(header.UserAgent) == "" {
		c.cfg.Defaults.Headers.Set(header.UserAgent, DefaultUserAgent)
	}
}

// resolveURL resolves relative path against client BaseURL or parses absolute URL strings.
func (c *Client) resolveURL(path string) (*url.URL, error) {
	u, err := urlkit.Resolve(c.prepared.BaseURL, path)
	if err != nil {
		return nil, &Error{Op: "resolve_url", Err: err}
	}

	return u, nil
}

func (c *Client) applyConfig(cfg Config) {
	c.cfg = cfg
	c.coreEngine = pipeline.NewEngine(cfg.Defaults.BaseURL, cfg.Defaults.Headers)
	c.prepared = c.coreEngine.Prepared
	c.baremetalEligible = c.cfg.IsBaremetalEligible()

	applyEngineConfig(c, cfg.Engine)

	c.applyDialers(c.Transport())
	c.reapplyH2Settings(c.Transport())
	c.applyPowerManagement(cfg.Network.EnablePowerManagement)

	c.pipeline = pipeline.New(
		c.toPipelineDefaults(),
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

func (c *Client) applyDefaultHTTPHeader() http.Header {
	if len(c.prepared.PrecomputedDefaultHeaders) == 0 {
		return make(http.Header)
	}

	reqHeader := make(http.Header, len(c.prepared.PrecomputedDefaultHeaders))
	for i := range c.prepared.PrecomputedDefaultHeaders {
		h := &c.prepared.PrecomputedDefaultHeaders[i]
		// Use a 3-index slice expression to set cap == len. This guarantees that any
		// append by net/http or middleware will allocate a fresh backing array rather
		// than writing into the shared PrecomputedDefaultHeaders slice, preventing
		// cross-request data corruption and potential data races under parallel load.
		s := h.Slice
		reqHeader[h.Key] = s[:len(s):len(s)]
	}

	return reqHeader
}

var (
	_ RequestDoer           = (*Client)(nil)
	_ HTTPRequester         = (*Client)(nil)
	_ Configurable[*Client] = (*Client)(nil)
)
