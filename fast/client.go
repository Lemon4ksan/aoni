// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"bytes"
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/internal/sys"
	"github.com/lemon4ksan/aoni/netutil/power"
	"github.com/lemon4ksan/aoni/telemetry"
)

// Client encapsulates an ultra-high-performance multi-protocol client
// seamlessly multiplexing native H1 (fasthttp), native H2 (h2engine), and native H3 (h3engine).
//
// Thread Safety & Concurrency:
// 100% thread-safe; safe for concurrent invocation across arbitrary goroutines.
//
// Memory Lifetime Invariants & Fast-Path Geometry:
// Achieves zero heap allocations on hot execution paths by recycling internal request/response buffers
// via sync.Pool. Callers MUST NOT retain or mutate byte slices obtained from unsafe body accessors beyond request lifecycle.
type Client struct {
	// engine encapsulates the underlying h1engine.Client providing extreme-throughput HTTP/1.1 socket pooling.
	engine *h1engine.Client

	// pipeline coordinates the 5-stage middleware, retry, hedging, and telemetry execution chain.
	pipeline *pipeline.Pipeline[aoni.Request, aoni.Response]

	// defaultDial holds the default network dialing function.
	defaultDial func(string) (net.Conn, error)

	// cfg is the immutable configuration snapshot for this client instance.
	cfg aoni.Config

	// powerWatcher listens for OS sleep/wake transitions to purge stale socket connections.
	powerWatcher *power.Watcher

	// referer tracks session navigation history to generate realistic Referer headers.
	referer *pipeline.RefererState

	// activeTargets tracks live in-flight hosts for connection throttling.
	activeTargets targetTracker

	// protocolState manages Alt-Svc cache, HTTP/2/3 availability, and protocol racing states.
	protocolState protocolState

	// coreEngine holds precomputed URL prefixes and immutable header byte representations.
	coreEngine *pipeline.Engine

	// prepared caches zero-allocation byte slices for fast-path URI matching.
	prepared pipeline.PreparedConfig

	// nativeDoer adapts fasthttp request execution into the generic pipeline.
	nativeDoer fastNativeDoer
}

// NewClient instantiates a multi-protocol ultra-high-throughput [Client] wrapping fasthttp, uTLS,
// native HTTP/2 framing, and native HTTP/3 QUIC support.
// Applies functional [aoni.ClientOption] layers sequentially to build prepared configuration state.
// Yields a ready-to-use, thread-safe [Client] pointer configured for zero-allocation execution.
func NewClient(opts ...aoni.ClientOption) *Client {
	c := &Client{
		engine: defaultFasthttpClient(),
		cfg: aoni.Config{
			Defaults: aoni.ClientDefaults{
				Headers: make(http.Header),
			},
		},
		referer:       &pipeline.RefererState{},
		protocolState: newProtocolState(),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&c.cfg)
		}
	}

	c.applyEngineConfig()
	c.applyCustomDialer()
	c.applyPowerManagement(c.cfg.Network.EnablePowerManagement)

	c.coreEngine = pipeline.NewEngine(c.cfg.Defaults.BaseURL, c.cfg.Defaults.Headers)
	c.prepared = c.coreEngine.Prepared
	c.prepared.FastPathCapable = (c.cfg.Engine.CookieJar == nil && c.cfg.Defaults.Inspector == nil)

	c.pipeline = pipeline.NewGeneric[aoni.Request, aoni.Response](
		toPipelineDefaults(c.cfg.Defaults, c.referer),
		c.cfg.Fingerprint.ToPipelineFingerprint(),
	)

	if len(c.cfg.Network.CPUAffinityCores) > 0 {
		sys.ApplyCPUAffinity(c.cfg.Network.CPUAffinityCores)
	}

	c.nativeDoer.client = c

	return c
}

// Clone creates an exact, memory-isolated duplicate of the current [Client] contract.
//
// It is an alias for c.With(), guaranteeing that the returned client is an independent
// contract with zero shared mutable state, preventing cross-goroutine interference.
func (c *Client) Clone() *Client {
	return c.With()
}

// With derives a brand new, fully autonomous [Client] contract with the provided functional options applied.
func (c *Client) With(opts ...aoni.ClientOption) *Client {
	if len(opts) == 0 {
		return c
	}

	cloned := &Client{
		engine:        cloneFasthttpClient(c.engine),
		defaultDial:   c.defaultDial,
		cfg:           c.cfg.Clone(),
		referer:       c.referer,
		protocolState: c.protocolState.Clone(),
	}

	cloned.nativeDoer.client = cloned

	for _, opt := range opts {
		if opt != nil {
			opt(&cloned.cfg)
		}
	}

	cloned.applyEngineConfig()

	if !isCustomDialerSet(c.engine, c.defaultDial) {
		cloned.applyCustomDialer()
	}

	cloned.applyPowerManagement(cloned.cfg.Network.EnablePowerManagement)

	cloned.coreEngine = pipeline.NewEngine(cloned.cfg.Defaults.BaseURL, cloned.cfg.Defaults.Headers)
	cloned.prepared = cloned.coreEngine.Prepared
	cloned.prepared.FastPathCapable = (cloned.cfg.Engine.CookieJar == nil && cloned.cfg.Defaults.Inspector == nil)

	cloned.pipeline = pipeline.NewGeneric[aoni.Request, aoni.Response](
		toPipelineDefaults(cloned.cfg.Defaults, cloned.referer),
		cloned.cfg.Fingerprint.ToPipelineFingerprint(),
	)

	return cloned
}

// ApplyOptions applies functional options to the client and returns a configured [aoni.RequestDoer].
func (c *Client) ApplyOptions(opts ...aoni.ClientOption) aoni.RequestDoer {
	return c.With(opts...)
}

// Get executes an HTTP GET request along the high-performance fasthttp pipeline.
func (c *Client) Get(ctx context.Context, path string, mods ...aoni.RequestModifier) (aoni.Response, error) {
	return c.Request(ctx, http.MethodGet, path, mods...)
}

// Post executes an HTTP POST request along the high-performance fasthttp pipeline.
func (c *Client) Post(ctx context.Context, path string, mods ...aoni.RequestModifier) (aoni.Response, error) {
	return c.Request(ctx, http.MethodPost, path, mods...)
}

// Put executes an HTTP PUT request along the high-performance fasthttp pipeline.
func (c *Client) Put(ctx context.Context, path string, mods ...aoni.RequestModifier) (aoni.Response, error) {
	return c.Request(ctx, http.MethodPut, path, mods...)
}

// Patch executes an HTTP PATCH request along the high-performance fasthttp pipeline.
func (c *Client) Patch(ctx context.Context, path string, mods ...aoni.RequestModifier) (aoni.Response, error) {
	return c.Request(ctx, http.MethodPatch, path, mods...)
}

// Delete executes an HTTP DELETE request along the high-performance fasthttp pipeline.
func (c *Client) Delete(ctx context.Context, path string, mods ...aoni.RequestModifier) (aoni.Response, error) {
	return c.Request(ctx, http.MethodDelete, path, mods...)
}

// Head executes an HTTP HEAD request along the high-performance fasthttp pipeline.
func (c *Client) Head(ctx context.Context, path string, mods ...aoni.RequestModifier) (aoni.Response, error) {
	return c.Request(ctx, http.MethodHead, path, mods...)
}

// Options executes an HTTP OPTIONS request along the high-performance fasthttp pipeline.
func (c *Client) Options(ctx context.Context, path string, mods ...aoni.RequestModifier) (aoni.Response, error) {
	return c.Request(ctx, http.MethodOptions, path, mods...)
}

// DoBaremetal executes a fast-path request bypassing middleware and pipeline layers.
func (c *Client) DoBaremetal(ctx context.Context, method, path string) (aoni.Response, error) {
	fastReq, fastResp := acquireFastPair()
	fastReq.Header.SetMethodBytes(getMethodBytes(method))

	if err := c.resolveTargetFastURI(fastReq, path); err != nil {
		releaseFastPair(fastReq, fastResp)

		return nil, err
	}

	for i := range c.prepared.PrecomputedDefaultHeaders {
		h := &c.prepared.PrecomputedDefaultHeaders[i]
		if len(fastReq.Header.PeekBytes(h.KeyBytes)) == 0 {
			fastReq.Header.SetBytesKV(h.KeyBytes, h.ValBytes)
		}
	}

	return c.executeFastPath(fastReq, fastResp)
}

// Request executes an HTTP request across HTTP/1.1, native HTTP/2, or native HTTP/3.
func (c *Client) Request(
	ctx context.Context,
	method, path string,
	mods ...aoni.RequestModifier,
) (aoni.Response, error) {
	if handler := c.resolveProtocolHandler(path); handler != nil {
		stdReq, err := http.NewRequestWithContext(ctx, method, path, nil)
		if err != nil {
			return nil, err
		}

		resp, err := handler.RoundTrip(stdReq) //nolint:bodyclose
		if err != nil {
			return nil, err
		}

		return aoni.NewStdResponse(resp), nil //nolint:bodyclose
	}

	fastReq, fastResp := acquireFastPair()
	fastReq.Header.SetMethodBytes(getMethodBytes(method))

	if len(mods) == 0 && c.prepared.FastPathCapable && (ctx == nil || ctx.Done() == nil) {
		if err := c.resolveTargetFastURI(fastReq, path); err != nil {
			releaseFastPair(fastReq, fastResp)

			return nil, err
		}

		for i := range c.prepared.PrecomputedDefaultHeaders {
			h := &c.prepared.PrecomputedDefaultHeaders[i]
			if len(fastReq.Header.PeekBytes(h.KeyBytes)) == 0 {
				fastReq.Header.SetBytesKV(h.KeyBytes, h.ValBytes)
			}
		}

		return c.executeFastPath(fastReq, fastResp)
	}

	reqAdapter := NewRequest(fastReq)
	reqAdapter.SetContext(ctx)

	if err := c.resolveTargetURL(reqAdapter, path); err != nil {
		reqAdapter.Release()
		releaseFastPair(fastReq, fastResp)

		return nil, err
	}

	c.applyDefaultHeaders(reqAdapter)

	if len(mods) > 0 {
		c.applyModifiers(reqAdapter, mods)
	}

	defer reqAdapter.Release()

	reqCtx := reqAdapter.Context()

	return c.pipeline.Execute(
		reqCtx,
		reqAdapter,
		&c.nativeDoer,
		c.resolvePipeline(reqCtx),
	)
}

func acquireFastPair() (*h1engine.Request, *h1engine.Response) {
	return h1engine.AcquireRequest(), h1engine.AcquireResponse()
}

func releaseFastPair(req *h1engine.Request, resp *h1engine.Response) {
	if req != nil {
		h1engine.ReleaseRequest(req)
	}

	if resp != nil {
		h1engine.ReleaseResponse(resp)
	}
}

func (c *Client) executeFastPath(fastReq *h1engine.Request, fastResp *h1engine.Response) (aoni.Response, error) {
	err := c.engine.Do(fastReq, fastResp)
	if err != nil {
		releaseFastPair(fastReq, fastResp)

		return nil, err
	}

	uncompressed := decompressFastResponse(fastResp)
	pr := NewPooledResponse(fastReq, fastResp)
	pr.SetUncompressed(uncompressed)

	return pr, nil
}

type fastNativeDoer struct {
	client *Client
}

func (f *fastNativeDoer) Do(req aoni.Request) (aoni.Response, error) {
	fastReq, ok := req.EngineRequest().(*h1engine.Request)
	if !ok || fastReq == nil {
		if stdReqObj := req.HTTPRequest(); stdReqObj != nil {
			stdResp, err := f.client.HTTP().Do(stdReqObj) //nolint:bodyclose
			if err != nil {
				return nil, err
			}

			return aoni.NewStdResponse(stdResp), nil //nolint:bodyclose
		}

		stdReq, err := http.NewRequestWithContext(
			req.Context(),
			req.Method(),
			req.URL(),
			req.BodyStream(),
		) //nolint:gosec
		if err != nil {
			return nil, err
		}

		stdResp, err := f.client.HTTP().Do(stdReq) //nolint:bodyclose
		if err != nil {
			return nil, err
		}

		return aoni.NewStdResponse(stdResp), nil //nolint:bodyclose
	}

	fastResp := h1engine.AcquireResponse()
	ctx := req.Context()

	trailers, err, autoReleased := f.client.executeWithRedirects(ctx, fastReq, fastResp)
	if err != nil {
		if !autoReleased {
			h1engine.ReleaseResponse(fastResp)
		}

		return nil, err
	}

	uncompressed := decompressFastResponse(fastResp)
	pr := NewPooledResponse(fastReq, fastResp)
	pr.SetUncompressed(uncompressed)

	if len(trailers) > 0 {
		pr.SetTrailers(trailers)
	}

	return pr, nil
}

// Do executes a prepared [aoni.Request] contract, routing through the target native protocol engine (H1, H2, or H3).
func (c *Client) Do(req aoni.Request) (aoni.Response, error) {
	if req == nil {
		req = NewRequest(nil)
	}

	if u := req.URL(); u != "" {
		_ = c.resolveTargetURL(req, u)
	}

	ctx := req.Context()

	return c.pipeline.Execute(ctx, req, &c.nativeDoer, c.resolvePipeline(ctx))
}

// Close shuts down idle TCP/TLS/H2/H3 connections and releases internal janitor background goroutines.
func (c *Client) Close() {
	if c.coreEngine != nil {
		c.coreEngine.Close()
	}

	if c.powerWatcher != nil {
		c.powerWatcher.Close()
		c.powerWatcher = nil
	}
}

// CloseIdleConnections purges all idle H1, H2, and H3 keep-alive sockets from connection pools.
func (c *Client) CloseIdleConnections() {
	if c.engine != nil {
		c.engine.CloseIdleConnections()
	}

	c.protocolState.h2Mutex.Lock()
	for host, h2Cl := range c.protocolState.h2Clients {
		delete(c.protocolState.h2Clients, host)

		_ = h2Cl
	}

	c.protocolState.h2Mutex.Unlock()

	if c.protocolState.h3Client != nil {
		_ = c.protocolState.h3Client.Close()
		c.protocolState.h3Client = nil
	}
}

// HTTP returns an [aoni.HTTPDoer] executing requests via fasthttp, H2, or H3.
func (c *Client) HTTP() aoni.HTTPDoer {
	return aoni.HTTPDoerFunc(func(req *http.Request) (*http.Response, error) {
		fastReq, fastResp := acquireFastPair()

		fastReq.SetRequestURI(req.URL.String())
		fastReq.Header.SetMethod(req.Method)
		fastReq.Header.SetHost(req.URL.Host)

		for k, vv := range req.Header {
			for _, v := range vv {
				fastReq.Header.Add(k, v)
			}
		}

		if req.Body != nil && req.Body != http.NoBody {
			buf := pipeline.GlobalBufferPool.Get()
			if _, err := buf.ReadFrom(req.Body); err == nil {
				fastReq.SetBody(buf.Bytes())
			}

			pipeline.GlobalBufferPool.Put(buf)
		}

		if ct := req.Header.Get("Content-Type"); ct != "" {
			fastReq.Header.SetContentType(ct)
		}

		ctx := req.Context()

		trailers, err, autoReleased := c.executeWithRedirects(ctx, fastReq, fastResp)
		if err != nil {
			if !autoReleased {
				releaseFastPair(fastReq, fastResp)
			}

			return nil, err
		}

		hadEncoding := len(fastResp.Header.Peek("Content-Encoding")) > 0
		uncompressed := decompressFastResponse(fastResp) || hadEncoding

		bodyRC := &fastBodyReadCloser{
			Reader:   bytes.NewReader(fastResp.Body()),
			fastReq:  fastReq,
			fastResp: fastResp,
		}

		httpResp := &http.Response{
			StatusCode:    fastResp.StatusCode(),
			Status:        http.StatusText(fastResp.StatusCode()),
			Header:        make(http.Header),
			Trailer:       make(http.Header),
			Body:          bodyRC,
			ContentLength: int64(len(fastResp.Body())),
			Uncompressed:  uncompressed,
			Request:       req,
		}

		for k, v := range fastResp.Header.All() {
			httpResp.Header.Add(string(k), string(v))
		}

		if len(trailers) > 0 {
			for k, vv := range trailers {
				for _, v := range vv {
					httpResp.Trailer.Add(k, v)
				}
			}
		}

		return httpResp, nil
	})
}

// AcquireRequest satisfies [aoni.RequestFactory] by acquiring a pooled [Request] instance.
func (c *Client) AcquireRequest() aoni.Request {
	return NewRequest(nil)
}

// ReleaseRequest satisfies [aoni.RequestFactory] by returning req back to the [sync.Pool] memory pool.
func (c *Client) ReleaseRequest(req aoni.Request) {
	if fastReq, ok := req.(*Request); ok {
		fastReq.Release()
	}
}

// Unwrap returns the underlying [*h1engine.Client] engine instance.
func (c *Client) Unwrap() *h1engine.Client {
	return c.engine
}

// Config returns a copy of active client configurations.
func (c *Client) Config() aoni.Config {
	return c.cfg.Clone()
}

// Defaults returns a copy of the default client configuration block.
func (c *Client) Defaults() aoni.ClientDefaults {
	return c.cfg.Defaults.Clone()
}

// Network returns a copy of the active network configuration block.
func (c *Client) Network() aoni.NetworkConfig {
	return c.cfg.Network.Clone()
}

// Fingerprint returns a copy of the TLS/HTTP fingerprint configuration block.
func (c *Client) Fingerprint() aoni.FingerprintConfig {
	return c.cfg.Fingerprint.Clone()
}

// EngineConfig returns the underlying transport engine configuration parameters.
func (c *Client) EngineConfig() aoni.EngineConfig {
	return c.cfg.Engine
}

// BaseURL returns the configured base target URL, or nil if unset.
func (c *Client) BaseURL() *url.URL {
	return c.cfg.Defaults.BaseURL
}

// BrowserID returns the active browser impersonation identity profile.
func (c *Client) BrowserID() aoni.BrowserID {
	return c.cfg.Fingerprint.BrowserID
}

// Inspector returns the configured network traffic inspector, or nil if unset.
func (c *Client) Inspector() telemetry.TrafficInspector {
	return c.cfg.Defaults.Inspector
}

// TLSConfig returns the configured TLS configuration parameters, or nil if unset.
func (c *Client) TLSConfig() *tls.Config {
	if c.engine != nil && c.engine.TLSConfig != nil {
		return c.engine.TLSConfig.Clone()
	}

	return nil
}

// Engine returns the underlying [*h1engine.Client] engine instance.
func (c *Client) Engine() *h1engine.Client {
	return c.engine
}

// Cookies retrieves non-expired cookies matching destination u from the active cookie jar.
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

	if pJar, ok := jar.(*cookie.ProxyIsolatedJar); ok {
		return pJar.FindCookie(u, name)
	}

	return generic.Find(jar.Cookies(u), func(ck *http.Cookie) bool {
		return ck != nil && ck.Name == name
	})
}

// FindCookieOptional searches for a cookie by name for a given URL and returns it wrapped in a [generic.Optional].
func (c *Client) FindCookieOptional(u *url.URL, name string) generic.Optional[*http.Cookie] {
	if ck, ok := c.FindCookie(u, name); ok {
		return generic.Some(ck)
	}

	return generic.None[*http.Cookie]()
}

// GetCookieValue retrieves the value of a named cookie.
func (c *Client) GetCookieValue(u *url.URL, name string) (string, bool) {
	if ck, ok := c.FindCookie(u, name); ok && ck != nil {
		return ck.Value, true
	}

	return "", false
}

// GetCookieValueOptional retrieves the value of a named cookie as a [generic.Optional].
func (c *Client) GetCookieValueOptional(u *url.URL, name string) generic.Optional[string] {
	if val, ok := c.GetCookieValue(u, name); ok {
		return generic.Some(val)
	}

	return generic.None[string]()
}

// LogValue implements [slog.LogValuer] for structured telemetry logging.
func (c *Client) LogValue() slog.Value {
	baseURLStr := ""
	if c.cfg.Defaults.BaseURL != nil {
		baseURLStr = c.cfg.Defaults.BaseURL.String()
	}

	return slog.GroupValue(
		slog.String("engine", "fasthttp"),
		slog.String("base_url", baseURLStr),
		slog.String("browser", c.cfg.Fingerprint.BrowserID.String()),
		slog.Duration("timeout", c.cfg.Engine.Timeout),
	)
}

func (c *Client) resolveProtocolHandler(rawURL string) http.RoundTripper {
	if len(c.cfg.Engine.Protocols) == 0 {
		return nil
	}

	scheme, _, ok := strings.Cut(rawURL, "://")
	if !ok {
		return nil
	}

	proto := aoni.Protocol(strings.ToLower(strings.TrimSpace(scheme)))
	if proto.IsStandardHTTP() {
		return nil
	}

	return c.cfg.Engine.Protocols[proto]
}

func (c *Client) applyEngineConfig() {
	if c.cfg.Engine.Timeout > 0 {
		c.engine.ReadTimeout = c.cfg.Engine.Timeout
		c.engine.WriteTimeout = c.cfg.Engine.Timeout
	}

	if c.cfg.Engine.InsecureSkipVerify {
		c.engine.TLSConfig = nil
	}

	c.engine.DisableHeaderNamesNormalizing = true
}

func (c *Client) applyCustomDialer() {
	d := c.Dial
	c.defaultDial = d
	c.engine.Dial = d
	c.engine.DialDualStack = true
}

func (c *Client) applyDefaultHeaders(req aoni.Request) {
	if len(c.prepared.PrecomputedDefaultHeaders) == 0 {
		return
	}

	if fastReqAdapter, ok := req.(*Request); ok {
		for i := range c.prepared.PrecomputedDefaultHeaders {
			h := &c.prepared.PrecomputedDefaultHeaders[i]
			if len(fastReqAdapter.HeaderBytes(h.KeyBytes)) == 0 {
				fastReqAdapter.SetHeaderBytes(h.KeyBytes, h.ValBytes)
			}
		}

		return
	}

	for i := range c.prepared.PrecomputedDefaultHeaders {
		h := &c.prepared.PrecomputedDefaultHeaders[i]
		if req.Header(h.Key) == "" {
			req.SetHeader(h.Key, h.Val)
		}
	}
}

func (c *Client) applyModifiers(req aoni.Request, mods []aoni.RequestModifier) {
	for _, m := range mods {
		m.Apply(req)
	}
}

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

func (c *Client) resolvePipeline(ctx context.Context) pipeline.PipelineConfig {
	if reqPipe, ok := pipeline.GetPipeline(ctx); ok {
		return reqPipe
	}

	pipe := c.cfg.Defaults.Pipeline
	if !pipe.RotateUA && len(c.cfg.Defaults.UARotationProfiles) > 0 {
		pipe.RotateUA = true
	}

	if pipe.SizeLimit == 0 {
		pipe.SizeLimit = c.cfg.Defaults.MaxResponseSize
	}

	if !pipe.Inspect && c.cfg.Defaults.Inspector != nil {
		pipe.Inspect = true
	}

	if pipe.Hedging == nil && (c.cfg.Network.HedgingDelay > 0 || c.cfg.Network.DynamicHedging != nil) {
		pipe.Hedging = &aoni.HedgingConfig{
			DefaultDelay:   c.cfg.Network.HedgingDelay,
			DynamicHedging: c.cfg.Network.DynamicHedging,
		}
	}

	return toInternalPipelineConfig(pipe)
}

func toPipelineDefaults(d aoni.ClientDefaults, referer *pipeline.RefererState) pipeline.ClientDefaults {
	var profiles []pipeline.BrowserProfile
	if len(d.UARotationProfiles) > 0 {
		profiles = make([]pipeline.BrowserProfile, len(d.UARotationProfiles))
		for i, p := range d.UARotationProfiles {
			profiles[i] = pipeline.BrowserProfile{
				UserAgent:   p.UserAgent,
				ClientHints: p.ClientHints,
			}
		}
	}

	return pipeline.ClientDefaults{
		Headers:              d.Headers,
		BeforeRequest:        d.BeforeRequest,
		AfterResponse:        d.AfterResponse,
		Inspector:            d.Inspector,
		ResponseValidator:    d.ResponseValidator,
		ChallengeDetector:    d.ChallengeDetector,
		ChallengeSolver:      d.ChallengeSolver,
		UARotationProfiles:   profiles,
		RefererState:         referer,
		MaxResponseSize:      d.MaxResponseSize,
		MultiReadThreshold:   d.MultiReadThreshold,
		MultiReadDisableDisk: d.MultiReadDisableDisk,
		RefererAutomaton:     d.RefererAutomaton,
	}
}

func toInternalPipelineConfig(p aoni.PipelineConfig) pipeline.PipelineConfig {
	return p.ToInternal()
}

var (
	methodGetBytes    = []byte("GET")
	methodPostBytes   = []byte("POST")
	methodPutBytes    = []byte("PUT")
	methodDeleteBytes = []byte("DELETE")
	methodPatchBytes  = []byte("PATCH")
	methodHeadBytes   = []byte("HEAD")
)

func getMethodBytes(method string) []byte {
	switch method {
	case "GET", "get":
		return methodGetBytes
	case "POST", "post":
		return methodPostBytes
	case "PUT", "put":
		return methodPutBytes
	case "DELETE", "delete":
		return methodDeleteBytes
	case "PATCH", "patch":
		return methodPatchBytes
	case "HEAD", "head":
		return methodHeadBytes
	default:
		return bytesconv.S2B(method)
	}
}

var (
	_ aoni.RequestDoer     = (*Client)(nil)
	_ aoni.WebSocketDialer = (*Client)(nil)
)
