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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
	"github.com/lemon4ksan/aoni/internal/engine"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/internal/rio"
	"github.com/lemon4ksan/aoni/netutil/power"
)

// HTTPDoer executes an HTTP request transaction.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client executes ultra-high-performance HTTP requests over fasthttp,
// seamlessly multiplexing native H1 (fasthttp), native H2 (h2engine), and native H3 (h3engine).
//
// Thread Safety & Concurrency:
// 100% thread-safe; safe for concurrent invocation across arbitrary goroutines.
//
// Memory Lifetime Invariants & Fast-Path Geometry:
// Achieves zero heap allocations on hot execution paths by recycling internal request/response buffers
// via sync.Pool. Callers MUST NOT retain or mutate byte slices obtained from unsafe body accessors beyond request lifecycle.
type Client struct {
	engine         *fasthttp.Client
	pipelineEngine *pipeline.Pipeline
	defaultDial    func(string) (net.Conn, error)
	config         aoni.Config
	powerWatcher   *power.Watcher
	referer        *pipeline.RefererState
	activeTargets  sync.Map

	protocolState protocolState
	coreEngine    *engine.Engine
	prepared      engine.PreparedConfig
}

// NewClient instantiates a multi-protocol ultra-high-throughput [Client] wrapping fasthttp, uTLS,
// native HTTP/2 framing, and native HTTP/3 QUIC support.
//
// Preconditions:
//   - Applies functional [aoni.ClientOption] layers sequentially to build prepared configuration state.
//
// Postconditions:
//   - Yields a ready-to-use, thread-safe [Client] pointer configured for zero-allocation execution.
func NewClient(opts ...aoni.ClientOption) *Client {
	c := &Client{
		engine: defaultFasthttpClient(),
		config: aoni.Config{
			Defaults: aoni.ClientDefaults{
				Headers: make(http.Header),
			},
		},
		referer: &pipeline.RefererState{},
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&c.config)
		}
	}

	c.applyEngineConfig()
	c.applyCustomDialer()
	c.applyPowerManagement(c.config.Network.EnablePowerManagement)

	c.coreEngine = engine.NewEngine(c.config.Defaults.BaseURL, c.config.Defaults.Headers, nil, 15*time.Second, 0)
	c.prepared = c.coreEngine.Prepared

	c.pipelineEngine = pipeline.NewPipeline(
		toPipelineDefaults(c.config.Defaults, c.referer),
		c.config.Fingerprint.ToPipelineFingerprint(),
	)

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

	c2 := &Client{
		engine:      clonedEngine,
		defaultDial: c.defaultDial,
		config:      c.config.Clone(),
		referer:     clonedReferer,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&c2.config)
		}
	}

	c2.applyEngineConfig()

	if !isCustomDialerSet(c.engine, c.defaultDial) {
		c2.applyCustomDialer()
	}

	c2.applyPowerManagement(c2.config.Network.EnablePowerManagement)

	c2.pipelineEngine = pipeline.NewPipeline(
		toPipelineDefaults(c2.config.Defaults, c2.referer),
		c2.config.Fingerprint.ToPipelineFingerprint(),
	)

	return c2
}

// Config returns a copy of active client configurations.
func (c *Client) Config() aoni.Config {
	return c.config
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
	if c.config.Defaults.Headers == nil {
		return
	}

	for k, vv := range c.config.Defaults.Headers {
		if req.Header(k) == "" && len(vv) > 0 {
			req.SetHeader(k, vv[0])
		}
	}
}

func (c *Client) applyModifiers(req aoni.Request, mods []aoni.RequestModifier) {
	for _, m := range mods {
		if m != nil {
			m(req)
		}
	}
}

// Engine returns the underlying [*fasthttp.Client] engine instance.
func (c *Client) Engine() *fasthttp.Client {
	return c.engine
}

// AcquireRequest satisfies [aoni.RequestFactory] by acquiring a pooled [Request] instance.
func (c *Client) AcquireRequest() aoni.Request {
	return NewRequest(nil)
}

// ReleaseRequest satisfies [aoni.RequestFactory] by returning req back to the memory pool.
func (c *Client) ReleaseRequest(req aoni.Request) {
	if fastReq, ok := req.(*Request); ok {
		fastReq.Release()
	}
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

func (c *Client) resolveTargetURL(req aoni.Request, path string) error {
	if fastReqAdapter, ok := req.(*Request); ok && len(c.prepared.BaseURLHostBytes) > 0 && len(path) > 0 &&
		path[0] == '/' &&
		!strings.Contains(path, "://") {
		fastReq := fastReqAdapter.req
		fastReq.URI().SetSchemeBytes(c.prepared.BaseURLSchemeBytes)
		fastReq.URI().SetHostBytes(c.prepared.BaseURLHostBytes)

		if c.config.Defaults.BaseURL != nil && c.config.Defaults.BaseURL.Path != "" &&
			c.config.Defaults.BaseURL.Path != "/" {
			basePath := strings.TrimSuffix(c.config.Defaults.BaseURL.Path, "/")
			fastReq.URI().SetPathBytes(bytesconv.S2B(basePath + path))
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

// Close shuts down idle connections and releases core engine janitor resources.
func (c *Client) Close() {
	if c.coreEngine != nil {
		c.coreEngine.Close()
	}

	if c.powerWatcher != nil {
		c.powerWatcher.Close()
		c.powerWatcher = nil
	}
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

func (c *Client) isFastPathEligible(ctx context.Context, mods []aoni.RequestModifier) bool {
	if len(mods) > 0 {
		return false
	}

	if ctx != nil && ctx.Done() != nil {
		return false
	}

	if c.config.Engine.CookieJar != nil || c.config.Defaults.Inspector != nil {
		return false
	}

	return true
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

		return aoni.NewStdResponse(resp), nil
	}

	fastReq := fasthttp.AcquireRequest()
	fastResp := fasthttp.AcquireResponse()

	fastReq.Header.SetMethodBytes(getMethodBytes(method))

	reqAdapter := NewRequest(fastReq)
	defer reqAdapter.Release()

	reqAdapter.SetContext(ctx)

	if err := c.resolveTargetURL(reqAdapter, path); err != nil {
		fasthttp.ReleaseRequest(fastReq)
		fasthttp.ReleaseResponse(fastResp)

		return nil, err
	}

	c.applyDefaultHeaders(reqAdapter)

	if c.isFastPathEligible(ctx, mods) {
		extractUserInfoAndSetAuth(fastReq)

		if c.config.Network.HasExperimental(aoni.ExpRIO) || c.config.Network.HasExperimental(aoni.ExpKernelBypass) {
			if reg, err := rio.RegisterBuffer(fastReq.Body()); err == nil && reg != nil {
				defer reg.Deregister()
			}
		}

		err := c.engine.Do(fastReq, fastResp)
		if err != nil {
			fasthttp.ReleaseRequest(fastReq)
			fasthttp.ReleaseResponse(fastResp)
			return nil, err
		}

		return NewPooledResponse(fastReq, fastResp), nil
	}

	reqAdapter = NewRequest(fastReq)
	defer reqAdapter.Release()

	reqAdapter.SetContext(ctx)
	c.applyModifiers(reqAdapter, mods)

	reqCtx := reqAdapter.Context()

	stdResp, err := c.pipelineEngine.Execute(reqCtx, reqAdapter, c.HTTP(), c.resolvePipeline(reqCtx)) //nolint:bodyclose
	if err != nil {
		fasthttp.ReleaseRequest(fastReq)
		fasthttp.ReleaseResponse(fastResp)

		return nil, err
	}

	return aoni.NewStdResponse(stdResp), nil
}

// Do executes a prepared [aoni.Request] contract, routing through the target native protocol engine (H1, H2, or H3).
func (c *Client) Do(req aoni.Request) (aoni.Response, error) {
	if req == nil {
		req = NewRequest(nil)
	}

	ctx := req.Context()

	stdResp, err := c.pipelineEngine.Execute(ctx, req, c.HTTP(), c.resolvePipeline(ctx)) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return aoni.NewStdResponse(stdResp), nil
}

// HTTP returns an [aoni.HTTPDoer] executing requests via fasthttp, H2, or H3.
func (c *Client) HTTP() aoni.HTTPDoer {
	return aoni.HTTPDoerFunc(func(req *http.Request) (*http.Response, error) {
		fastReq := fasthttp.AcquireRequest()
		fastResp := fasthttp.AcquireResponse()

		fastReq.Header.SetMethod(req.Method)
		fastReq.SetRequestURI(req.URL.String())
		fastReq.Header.SetHost(req.URL.Host)

		for k, vv := range req.Header {
			for _, v := range vv {
				fastReq.Header.Add(k, v)
			}
		}

		if req.Body != nil && req.Body != http.NoBody {
			contentLen := req.ContentLength
			if contentLen <= 0 {
				if clStr := req.Header.Get("Content-Length"); clStr != "" {
					if parsed, err := strconv.ParseInt(strings.TrimSpace(clStr), 10, 64); err == nil {
						contentLen = parsed
					}
				}
			}

			fastReq.SetBodyStream(req.Body, int(contentLen))
		}

		ctx := req.Context()

		trailers, err, autoReleased := c.executeWithRedirects(ctx, fastReq, fastResp)
		if err != nil {
			if !autoReleased {
				fasthttp.ReleaseRequest(fastReq)
				fasthttp.ReleaseResponse(fastResp)
			}

			return nil, err
		}

		uncompressed := decompressFastResponse(fastResp)

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
			httpResp.Header.Add(string(k), string(v))
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

		if err := applyRedirectMethodAndBody(statusCode, fastReq); err != nil {
			return nil, err, false
		}

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

func applyRedirectMethodAndBody(statusCode int, req *fasthttp.Request) error {
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

	return nil
}

func decompressFastResponse(resp *fasthttp.Response) bool {
	enforceContentLengthTruncation(resp)

	encodingBytes := resp.Header.Peek("Content-Encoding")
	if len(encodingBytes) == 0 {
		return false
	}

	encoding := strings.ToLower(bytesconv.B2S(encodingBytes))

	body := resp.Body()
	if len(body) == 0 {
		return false
	}

	var (
		decompressed []byte
		err          error
	)

	switch {
	case strings.Contains(encoding, "gzip"):
		gzReader, gzErr := gzip.NewReader(bytes.NewReader(body))
		if gzErr == nil {
			decompressed, err = io.ReadAll(gzReader)
			_ = gzReader.Close()
		}

	case strings.Contains(encoding, "br"):
		brReader := brotli.NewReader(bytes.NewReader(body))
		decompressed, err = io.ReadAll(brReader)

	case strings.Contains(encoding, "zstd"):
		if zDec, zErr := zstd.NewReader(bytes.NewReader(body)); zErr == nil {
			decompressed, err = io.ReadAll(zDec)
			zDec.Close()
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

	clBytes := resp.Header.Peek("Content-Length")
	if len(clBytes) == 0 {
		return
	}

	cl, err := strconv.ParseInt(bytesconv.B2S(clBytes), 10, 64)
	if err != nil || cl < 0 {
		return
	}

	if !resp.IsBodyStream() {
		body := resp.Body()
		if int64(len(body)) > cl {
			resp.SetBody(body[:cl])
		}

		return
	}

	if stream := resp.BodyStream(); stream != nil {
		resp.SetBodyStream(io.LimitReader(stream, cl), int(cl))
	}
}

func (c *Client) resolvePipeline(ctx context.Context) pipeline.PipelineConfig {
	if reqPipe, ok := aoni.GetPipeline(ctx); ok {
		return toInternalPipelineConfig(reqPipe)
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
	res := pipeline.PipelineConfig{
		SizeLimit:          p.SizeLimit,
		MultiReadThreshold: p.MultiReadThreshold,
		RotateUA:           p.RotateUA,
		Inspect:            p.Inspect,
		Decompress:         p.Decompress,
		Validate:           p.Validate,
		Challenge:          p.Challenge,
	}
	if p.DPIJitter != nil {
		res.DPIJitter = &pipeline.DPIJitterConfig{
			MinDelay: p.DPIJitter.MinDelay,
			MaxDelay: p.DPIJitter.MaxDelay,
		}
	}

	if p.ProxyFailover != nil {
		res.ProxyFailover = &pipeline.ProxyFailoverConfig{
			Proxies:    p.ProxyFailover.Proxies,
			RetryLimit: p.ProxyFailover.RetryLimit,
		}
	}

	if p.Hedging != nil {
		res.Hedging = &pipeline.HedgingConfig{
			DynamicHedging:       p.Hedging.DynamicHedging,
			DefaultDelay:         p.Hedging.DefaultDelay,
			MaxRequestsPerSecond: p.Hedging.MaxRequestsPerSecond,
			AllowNonReadOnly:     p.Hedging.AllowNonReadOnly,
		}
	}

	if p.Cache != nil {
		var nvs *pipeline.NoVarySearchConfig
		if p.Cache.NoVarySearch != nil {
			nvs = &pipeline.NoVarySearchConfig{
				IgnoreParams:    p.Cache.NoVarySearch.IgnoreParams,
				ExceptParams:    p.Cache.NoVarySearch.ExceptParams,
				IgnoreAllParams: p.Cache.NoVarySearch.IgnoreAllParams,
			}
		}

		res.Cache = &pipeline.CacheConfig{
			Store:         p.Cache.Store,
			DefaultTTL:    p.Cache.DefaultTTL,
			NoVarySearch:  nvs,
			CookieIndices: p.Cache.CookieIndices,
		}
	}

	if p.HAR != nil {
		res.HAR = &pipeline.HARConfig{
			Tracker: p.HAR.Tracker,
		}
	}

	if p.Redact != nil {
		res.Redact = &pipeline.RedactConfig{
			Headers:          p.Redact.Headers,
			HeadersToRedact:  p.Redact.HeadersToRedact,
			JSONKeysToRedact: p.Redact.JSONKeysToRedact,
		}
	}

	res.BuildFlags()

	return res
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

var (
	_ aoni.RequestDoer = (*Client)(nil)
	_ aoni.WSDialer    = (*Client)(nil)
)
