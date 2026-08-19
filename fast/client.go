// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fast provides high-performance fasthttp engine adapters for [aoni.Request] and [aoni.Response].
package fast

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/experimental"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/netutil/power"
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
	engine        *fasthttp.Client
	pipeline      *pipeline.Pipeline[aoni.Request, aoni.Response]
	defaultDial   func(string) (net.Conn, error)
	config        aoni.Config
	powerWatcher  *power.Watcher
	referer       *pipeline.RefererState
	activeTargets targetTracker

	protocolState protocolState
	coreEngine    *pipeline.Engine
	prepared      pipeline.PreparedConfig
	nativeDoer    fastNativeDoer
}

// NewClient instantiates a multi-protocol ultra-high-throughput [Client] wrapping fasthttp, uTLS,
// native HTTP/2 framing, and native HTTP/3 QUIC support.
// Applies functional [aoni.ClientOption] layers sequentially to build prepared configuration state.
// Yields a ready-to-use, thread-safe [Client] pointer configured for zero-allocation execution.
func NewClient(opts ...aoni.ClientOption) *Client {
	c := &Client{
		engine: defaultFasthttpClient(),
		config: aoni.Config{
			Defaults: aoni.ClientDefaults{
				Headers: make(http.Header),
			},
		},
		referer:       &pipeline.RefererState{},
		protocolState: newProtocolState(),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&c.config)
		}
	}

	c.applyEngineConfig()
	c.applyCustomDialer()
	c.applyPowerManagement(c.config.Network.EnablePowerManagement)

	c.coreEngine = pipeline.NewEngine(c.config.Defaults.BaseURL, c.config.Defaults.Headers)
	c.prepared = c.coreEngine.Prepared
	c.prepared.FastPathCapable = (c.config.Engine.CookieJar == nil && c.config.Defaults.Inspector == nil)

	c.pipeline = pipeline.NewGeneric[aoni.Request, aoni.Response](
		toPipelineDefaults(c.config.Defaults, c.referer),
		c.config.Fingerprint.ToPipelineFingerprint(),
	)

	if len(c.config.Network.CPUAffinityCores) > 0 {
		experimental.ApplyCPUAffinity(c.config.Network.CPUAffinityCores)
	}

	c.nativeDoer.client = c

	return c
}

// With produces a deep-copied [Client] with the provided functional options applied.
func (c *Client) With(opts ...aoni.ClientOption) *Client {
	clonedEngine := cloneFasthttpClient(c.engine)

	clonedReferer := &pipeline.RefererState{}
	if c.referer != nil {
		c.referer.Mu.Lock()
		clonedReferer.LastURL = c.referer.LastURL
		c.referer.Mu.Unlock()
	}

	cloned := &Client{
		engine:        clonedEngine,
		defaultDial:   c.defaultDial,
		config:        c.config.Clone(),
		referer:       clonedReferer,
		protocolState: c.protocolState.Clone(),
	}

	cloned.nativeDoer.client = cloned

	for _, opt := range opts {
		if opt != nil {
			opt(&cloned.config)
		}
	}

	cloned.applyEngineConfig()

	if !isCustomDialerSet(c.engine, c.defaultDial) {
		cloned.applyCustomDialer()
	}

	cloned.applyPowerManagement(cloned.config.Network.EnablePowerManagement)

	cloned.coreEngine = pipeline.NewEngine(cloned.config.Defaults.BaseURL, cloned.config.Defaults.Headers)
	cloned.prepared = cloned.coreEngine.Prepared
	cloned.prepared.FastPathCapable = (cloned.config.Engine.CookieJar == nil && cloned.config.Defaults.Inspector == nil)

	cloned.pipeline = pipeline.NewGeneric[aoni.Request, aoni.Response](
		toPipelineDefaults(cloned.config.Defaults, cloned.referer),
		cloned.config.Fingerprint.ToPipelineFingerprint(),
	)

	return cloned
}

// ApplyOptions applies functional options to the client and returns a configured [aoni.RequestDoer].
func (c *Client) ApplyOptions(opts ...aoni.ClientOption) aoni.RequestDoer {
	return c.With(opts...)
}

// Request executes an HTTP request across HTTP/1.1, native HTTP/2, or native HTTP/3.
//
// Preconditions:
//   - ctx MUST NOT be nil (pass [context.Background] if no timeout is desired).
//   - method SHOULD be a standard HTTP method ("GET", "POST", etc.) or custom string.
//
// Yields an [aoni.Response] contract backed by pooled response memory.
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

func acquireFastPair() (*fasthttp.Request, *fasthttp.Response) {
	return fasthttp.AcquireRequest(), fasthttp.AcquireResponse()
}

func releaseFastPair(req *fasthttp.Request, resp *fasthttp.Response) {
	if req != nil {
		fasthttp.ReleaseRequest(req)
	}

	if resp != nil {
		fasthttp.ReleaseResponse(resp)
	}
}

func (c *Client) executeFastPath(fastReq *fasthttp.Request, fastResp *fasthttp.Response) (aoni.Response, error) {
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
	fastReq, ok := req.EngineRequest().(*fasthttp.Request)
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

	fastResp := fasthttp.AcquireResponse()
	ctx := req.Context()

	trailers, err, autoReleased := f.client.executeWithRedirects(ctx, fastReq, fastResp)
	if err != nil {
		if !autoReleased {
			fasthttp.ReleaseResponse(fastResp)
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
//
// Thread Safety & Cleanup Contract:
//   - Thread-safe; safe to call concurrently or during client shutdown.
//   - Releases internal ring buffer pools, idle H2 connections, and background power management watchers.
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

		fastResp.Header.All()(func(k, v []byte) bool {
			httpResp.Header.Add(bytesconv.B2S(k), bytesconv.B2S(v))
			return true
		})

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
// Safe for concurrent invocation across arbitrary goroutines.
// Callers MUST release the acquired request via [Client.ReleaseRequest] or [Request.Release] when finished.
// Yields a zero-allocation pooled [Request] ready for payload initialization.
func (c *Client) AcquireRequest() aoni.Request {
	return NewRequest(nil)
}

// ReleaseRequest satisfies [aoni.RequestFactory] by returning req back to the [sync.Pool] memory pool.
// The request object is zeroed out and returned to the pool. Callers MUST NOT reference req after releasing.
func (c *Client) ReleaseRequest(req aoni.Request) {
	if fastReq, ok := req.(*Request); ok {
		fastReq.Release()
	}
}

// Config returns a copy of active client configurations.
func (c *Client) Config() aoni.Config {
	return c.config
}

// Engine returns the underlying [*fasthttp.Client] engine instance.
func (c *Client) Engine() *fasthttp.Client {
	return c.engine
}

func (c *Client) resolveProtocolHandler(rawURL string) http.RoundTripper {
	if len(c.config.Engine.Protocols) == 0 {
		return nil
	}

	scheme, _, ok := strings.Cut(rawURL, "://")
	if !ok {
		return nil
	}

	normScheme := strings.ToLower(strings.TrimSpace(scheme))
	if normScheme == "http" || normScheme == "https" || normScheme == "ws" || normScheme == "wss" {
		return nil
	}

	return c.config.Engine.Protocols[normScheme]
}

func (c *Client) resolveTargetFastURI(fastReq *fasthttp.Request, path string) error {
	if len(c.prepared.BaseURLHostBytes) > 0 && len(path) > 0 && path[0] == '/' && (len(path) < 2 || path[1] != '/') {
		fastReq.URI().SetSchemeBytes(c.prepared.BaseURLSchemeBytes)
		fastReq.URI().SetHostBytes(c.prepared.BaseURLHostBytes)

		if len(c.prepared.BaseURLCleanPathBytes) > 0 {
			var stackBuf [256]byte

			needed := len(c.prepared.BaseURLCleanPathBytes) + len(path)

			var pathBuf []byte
			if needed <= len(stackBuf) {
				pathBuf = stackBuf[:0]
			} else {
				pathBuf = make([]byte, 0, needed)
			}

			pathBuf = append(pathBuf, c.prepared.BaseURLCleanPathBytes...)
			pathBuf = append(pathBuf, path...)
			fastReq.URI().SetPathBytes(pathBuf)
		} else {
			fastReq.URI().SetPathBytes(bytesconv.S2B(path))
		}

		return nil
	}

	return c.resolveTargetURLFastFallback(fastReq, path)
}

func (c *Client) resolveTargetURLFastFallback(fastReq *fasthttp.Request, path string) error {
	var targetURL string

	switch {
	case len(path) >= 7 && (strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")):
		targetURL = path
	case c.prepared.BaseURLTrimmedString != "":
		switch path == "" || path == "/" {
		case true:
			targetURL = c.prepared.BaseURLString
		case false:
			if path[0] == '/' {
				targetURL = c.prepared.BaseURLTrimmedString + path
			} else {
				targetURL = c.prepared.BaseURLTrimmedString + "/" + path
			}
		}

	case c.config.Defaults.BaseURL != nil && c.config.Defaults.BaseURL.Host != "":
		base := c.config.Defaults.BaseURL
		basePath := strings.TrimSuffix(base.Path, "/")

		cleanPath := path
		if cleanPath != "" && cleanPath[0] != '/' {
			cleanPath = "/" + cleanPath
		}

		targetURL = base.Scheme + "://" + base.Host + basePath + cleanPath

	case path == "":
		return ErrTargetURLEmpty
	default:
		targetURL = path
	}

	fastReq.SetRequestURI(targetURL)

	return nil
}

func (c *Client) resolveTargetURL(req aoni.Request, path string) error {
	if fastReqAdapter, ok := req.(*Request); ok && len(c.prepared.BaseURLHostBytes) > 0 && len(path) > 0 &&
		path[0] == '/' &&
		(len(path) < 2 || path[1] != '/') {
		fastReq := fastReqAdapter.req
		fastReq.URI().SetSchemeBytes(c.prepared.BaseURLSchemeBytes)
		fastReq.URI().SetHostBytes(c.prepared.BaseURLHostBytes)

		if len(c.prepared.BaseURLCleanPathBytes) > 0 {
			var stackBuf [256]byte

			needed := len(c.prepared.BaseURLCleanPathBytes) + len(path)

			var pathBuf []byte
			if needed <= len(stackBuf) {
				pathBuf = stackBuf[:0]
			} else {
				pathBuf = make([]byte, 0, needed)
			}

			pathBuf = append(pathBuf, c.prepared.BaseURLCleanPathBytes...)
			pathBuf = append(pathBuf, path...)
			fastReq.URI().SetPathBytes(pathBuf)
		} else {
			fastReq.URI().SetPathBytes(bytesconv.S2B(path))
		}

		return nil
	}

	var targetURL string
	switch {
	case len(path) >= 7 && (strings.HasPrefix(path, "http://") ||
		strings.HasPrefix(path, "https://")):
		targetURL = path
	case c.prepared.BaseURLTrimmedString != "":
		switch path == "" || path == "/" {
		case true:
			targetURL = c.prepared.BaseURLString
		case false:
			if path[0] == '/' {
				targetURL = c.prepared.BaseURLTrimmedString + path
			} else {
				targetURL = c.prepared.BaseURLTrimmedString + "/" + path
			}
		}

	case c.config.Defaults.BaseURL != nil && c.config.Defaults.BaseURL.Host != "":
		base := c.config.Defaults.BaseURL
		basePath := strings.TrimSuffix(base.Path, "/")

		cleanPath := path
		if cleanPath != "" && cleanPath[0] != '/' {
			cleanPath = "/" + cleanPath
		}

		targetURL = base.Scheme + "://" + base.Host + basePath + cleanPath

	case path == "":
		return ErrTargetURLEmpty
	default:
		targetURL = path
	}

	req.SetURL(targetURL)

	if strings.Contains(targetURL, "@") {
		if parsed, err := url.Parse(targetURL); err == nil && parsed.User != nil {
			username := parsed.User.Username()
			password, _ := parsed.User.Password()
			auth := username + ":" + password

			basicAuth := "Basic " + base64.StdEncoding.EncodeToString(bytesconv.S2B(auth))
			if req.Header("Authorization") == "" {
				req.SetHeader("Authorization", basicAuth)
			}
		}
	}

	return nil
}

func (c *Client) executeWithRedirects(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	redirectLimit := c.config.Engine.RedirectLimit
	if redirectLimit < 0 {
		redirectLimit = 10
	}

	if redirectLimit == 0 {
		c.applyCookies(ctx, fastReq)
		extractUserInfoAndSetAuth(fastReq)

		trailers, err, autoReleased = c.dispatchSingleRequest(ctx, fastReq, fastResp)
		if err == nil {
			c.captureCookies(ctx, fastReq, fastResp)
		}

		return trailers, err, autoReleased
	}

	currentURI := fasthttp.AcquireURI()
	defer fasthttp.ReleaseURI(currentURI)

	var redirectsFollowed int

	for {
		c.applyCookies(ctx, fastReq)
		fastReq.URI().CopyTo(currentURI)
		extractUserInfoAndSetAuth(fastReq)

		trailers, err, autoReleased = c.dispatchSingleRequest(ctx, fastReq, fastResp)
		if err != nil {
			return nil, err, autoReleased
		}

		c.captureCookies(ctx, fastReq, fastResp)

		statusCode := fastResp.StatusCode()
		if !isRedirectStatus(statusCode) {
			return trailers, nil, false
		}

		location := fastResp.Header.Peek("Location")
		if len(location) == 0 {
			return trailers, nil, false
		}

		redirectsFollowed++
		if redirectsFollowed > redirectLimit {
			return nil, ErrMaxRedirectsExceeded, false
		}

		applyRedirectMethodAndBody(statusCode, fastReq)

		method := bytes.Clone(fastReq.Header.Method())
		nextURI := fasthttp.AcquireURI()
		currentURI.CopyTo(nextURI)
		nextURI.UpdateBytes(location)

		if len(nextURI.Scheme()) == 0 {
			nextURI.SetSchemeBytes(currentURI.Scheme())
		}

		if len(nextURI.Host()) == 0 {
			nextURI.SetHostBytes(currentURI.Host())
		}

		nextURI.CopyTo(fastReq.URI())
		fastReq.Header.SetRequestURIBytes(nextURI.RequestURI())

		if len(method) > 0 {
			fastReq.Header.SetMethodBytes(method)
		}

		if host := nextURI.Host(); len(host) > 0 {
			fastReq.Header.SetHostBytes(host)
		}

		if !isSameHost(currentURI, nextURI) {
			scrubSensitiveHeaders(fastReq, currentURI, nextURI)
		}

		if isHTTPSDowngrade(currentURI, nextURI) {
			fastReq.Header.Del("Referer")
		} else {
			fastReq.Header.SetBytesK(bytesconv.S2B("Referer"), string(currentURI.FullURI()))
		}

		fasthttp.ReleaseURI(nextURI)
		fastResp.Reset()
	}
}

func (c *Client) applyEngineConfig() {
	if c.config.Engine.Timeout > 0 {
		c.engine.ReadTimeout = c.config.Engine.Timeout
		c.engine.WriteTimeout = c.config.Engine.Timeout
	}

	if c.config.Engine.InsecureSkipVerify {
		c.engine.TLSConfig = nil
	}

	c.engine.DisableHeaderNamesNormalizing = true
}

func (c *Client) applyCustomDialer() {
	c.defaultDial = c.Dial
	c.engine.Dial = c.Dial
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

	pipe := c.config.Defaults.Pipeline
	if !pipe.RotateUA && len(c.config.Defaults.UARotationProfiles) > 0 {
		pipe.RotateUA = true
	}

	if pipe.SizeLimit == 0 {
		pipe.SizeLimit = c.config.Defaults.MaxResponseSize
	}

	if !pipe.Inspect && c.config.Defaults.Inspector != nil {
		pipe.Inspect = true
	}

	if pipe.Hedging == nil && (c.config.Network.HedgingDelay > 0 || c.config.Network.DynamicHedging != nil) {
		pipe.Hedging = &aoni.HedgingConfig{
			DefaultDelay:   c.config.Network.HedgingDelay,
			DynamicHedging: c.config.Network.DynamicHedging,
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

func isRedirectStatus(code int) bool {
	return code == fasthttp.StatusMovedPermanently ||
		code == fasthttp.StatusFound ||
		code == fasthttp.StatusSeeOther ||
		code == fasthttp.StatusTemporaryRedirect ||
		code == fasthttp.StatusPermanentRedirect
}

func isSameHost(u1, u2 *fasthttp.URI) bool {
	return bytes.EqualFold(u1.Host(), u2.Host())
}

func isHTTPSDowngrade(u1, u2 *fasthttp.URI) bool {
	return bytes.EqualFold(u1.Scheme(), []byte("https")) && bytes.EqualFold(u2.Scheme(), []byte("http"))
}

func applyRedirectMethodAndBody(statusCode int, req *fasthttp.Request) {
	switch statusCode {
	case fasthttp.StatusMovedPermanently, fasthttp.StatusFound, fasthttp.StatusSeeOther:
		method := string(req.Header.Method())
		if method != http.MethodGet && method != http.MethodHead {
			req.Header.SetMethod(http.MethodGet)
			req.SetBody(nil)
			req.Header.Del("Content-Type")
			req.Header.Del("Content-Length")
		}
	}
}

var zstdDecoderPool = sync.Pool{
	New: func() any {
		dec, _ := zstd.NewReader(nil)
		return dec
	},
}

func decompressFastResponse(resp *fasthttp.Response) bool {
	enforceContentLengthTruncation(resp)

	encodingBytes := resp.Header.ContentEncoding()
	if len(encodingBytes) == 0 {
		return false
	}

	body := resp.Body()
	if len(body) == 0 {
		return false
	}

	var (
		decompressed []byte
		err          error
	)

	switch {
	case bytesconv.ContainsFoldASCII(encodingBytes, "gzip"):
		decompressed, err = resp.BodyGunzip()

	case bytesconv.ContainsFoldASCII(encodingBytes, "br"):
		decompressed, err = resp.BodyUnbrotli()

	case bytesconv.ContainsFoldASCII(encodingBytes, "zstd"):
		if dec, ok := zstdDecoderPool.Get().(*zstd.Decoder); ok && dec != nil {
			decompressed, err = dec.DecodeAll(body, nil)
			zstdDecoderPool.Put(dec)
		}
	}

	if err == nil && len(decompressed) > 0 {
		resp.SetBody(decompressed)
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")

		return true
	}

	return false
}

func enforceContentLengthTruncation(resp *fasthttp.Response) {
	if resp == nil {
		return
	}

	cl := resp.Header.ContentLength()
	if cl < 0 {
		return
	}

	if !resp.IsBodyStream() {
		body := resp.Body()
		if len(body) > cl {
			resp.SetBody(body[:cl])
		}

		return
	}

	if stream := resp.BodyStream(); stream != nil {
		resp.SetBodyStream(io.LimitReader(stream, int64(cl)), cl)
	}
}

var (
	_ aoni.RequestDoer     = (*Client)(nil)
	_ aoni.WebSocketDialer = (*Client)(nil)
)
