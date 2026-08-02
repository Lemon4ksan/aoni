// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fast provides high-performance fasthttp engine adapters for [aoni.Request] and [aoni.Response].
package fast

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/quic-go/quic-go"
	"github.com/valyala/fasthttp"
	"golang.org/x/sys/cpu"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fast/h2engine"
	"github.com/lemon4ksan/aoni/fast/h3engine"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/fragment"
)

var fastZstdDecoder, _ = zstd.NewReader(nil)

// HTTPDoer executes an HTTP request transaction.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client executes ultra-high-performance HTTP requests over fasthttp,
// seamlessly multiplexing native H1 (fasthttp), native H2 (h2engine), and native H3 (h3engine).
type Client struct {
	engine    *fasthttp.Client
	h2Clients map[string]*h2engine.Client
	h3Client  *h3engine.Client
	config    aoni.Config

	_       cpu.CacheLinePad
	h2Mutex sync.Mutex
	_       cpu.CacheLinePad
	h3Once  sync.Once
	_       cpu.CacheLinePad
}

// NewClient creates a new multiprotocol Client configured with fasthttp, uTLS,
// native HTTP/2 framing, and native HTTP/3 QUIC support.
func NewClient(opts ...aoni.ClientOption) *Client {
	c := &Client{
		engine: &fasthttp.Client{
			ReadTimeout:         0,
			WriteTimeout:        0,
			MaxConnsPerHost:     512,
			MaxIdleConnDuration: 90 * time.Second,
		},
		config: aoni.Config{
			Defaults: aoni.ClientDefaults{
				Headers: make(http.Header),
			},
		},
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&c.config)
		}
	}

	c.applyEngineConfig()
	c.applyCustomDialer()

	return c
}

// With produces a deep-copied [Client] with the provided functional options applied.
func (c *Client) With(opts ...aoni.ClientOption) *Client {
	c2 := &Client{
		engine: c.engine,
		config: c.config.Clone(),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&c2.config)
		}
	}

	c2.applyEngineConfig()
	c2.applyCustomDialer()

	return c2
}

// Config returns a copy of active client configurations.
func (c *Client) Config() aoni.Config {
	return c.config
}

// Engine returns the underlying [*fasthttp.Client] engine instance.
func (c *Client) Engine() *fasthttp.Client {
	return c.engine
}

// Request executes an HTTP request across HTTP/1.1, native HTTP/2, or native HTTP/3.
// Handles redirects, cookies, proxy failover, hedging, response validation, WAF challenges, and telemetry.
//
// Postconditions:
//   - The returned [aoni.Response] MUST be closed via [aoni.Response.Close]
//     to return objects back to [sync.Pool].
//   - Aborts execution immediately if ctx is canceled without memory race hazards.
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

	reqAdapter := NewRequest(fastReq)
	defer reqAdapter.Release()

	reqAdapter.SetContext(ctx)
	reqAdapter.SetMethod(method)

	if err := c.resolveTargetURL(reqAdapter, path); err != nil {
		fasthttp.ReleaseRequest(fastReq)
		fasthttp.ReleaseResponse(fastResp)

		return nil, err
	}

	reqCfg := aoni.GetOrInitRequestConfig(ctx)
	if reqCfg.TargetHost == "" {
		if u, err := url.Parse(reqAdapter.URL()); err == nil && u.Hostname() != "" {
			reqCfg.TargetHost = u.Hostname()
		} else if hostStr := string(fastReq.URI().Host()); hostStr != "" {
			h, _, _ := net.SplitHostPort(hostStr)
			if h == "" {
				h = hostStr
			}

			reqCfg.TargetHost = h
		}
	}

	c.applyDefaultHeaders(reqAdapter)
	c.applyModifiers(reqAdapter, mods)

	reqCtx := reqAdapter.Context()

	trailers, uncompressed, err, autoReleased := c.dispatchPipeline(reqCtx, fastReq, fastResp, reqAdapter)
	if err != nil {
		if !autoReleased {
			fasthttp.ReleaseRequest(fastReq)
			fasthttp.ReleaseResponse(fastResp)
		}

		return nil, err
	}

	pooledResp := NewPooledResponse(fastReq, fastResp)

	if len(trailers) > 0 {
		pooledResp.SetTrailers(trailers)
	}

	if uncompressed {
		pooledResp.SetUncompressed(true)
	}

	return pooledResp, nil
}

// Do executes a prepared [aoni.Request] contract, routing through the target
// native protocol engine (H1, H2, or H3).
func (c *Client) Do(req aoni.Request) (aoni.Response, error) {
	if req == nil {
		req = NewRequest(nil)
	}

	ctx := req.Context()
	fastReq, isFastReq := req.EngineRequest().(*fasthttp.Request)

	var reqAdapter *Request
	if fastAdapter, ok := req.(*Request); ok {
		reqAdapter = fastAdapter
	}

	if !isFastReq {
		fastReq = fasthttp.AcquireRequest()
		fastReq.Header.SetMethod(req.Method())
		fastReq.SetRequestURI(req.URL())

		if bodyStream := req.BodyStream(); bodyStream != nil {
			fastReq.SetBodyStream(bodyStream, -1)
		} else if body := req.BodyBytes(); len(body) > 0 {
			fastReq.SetBody(body)
		}
	}

	reqCfg := aoni.GetOrInitRequestConfig(ctx)
	if reqCfg.TargetHost == "" {
		if u, err := url.Parse(req.URL()); err == nil && u.Hostname() != "" {
			reqCfg.TargetHost = u.Hostname()
		} else if hostStr := string(fastReq.URI().Host()); hostStr != "" {
			h, _, _ := net.SplitHostPort(hostStr)
			if h == "" {
				h = hostStr
			}

			reqCfg.TargetHost = h
		}
	}

	fastResp := fasthttp.AcquireResponse()

	trailers, uncompressed, err, autoReleased := c.dispatchPipeline(ctx, fastReq, fastResp, reqAdapter)
	if err != nil {
		if !autoReleased {
			fasthttp.ReleaseResponse(fastResp)

			if !isFastReq {
				fasthttp.ReleaseRequest(fastReq)
			}
		}

		return nil, err
	}

	respAdapter := NewResponse(fastResp)
	if len(trailers) > 0 {
		respAdapter.SetTrailers(trailers)
	}

	if uncompressed {
		respAdapter.SetUncompressed(true)
	}

	return respAdapter, nil
}

// DialContext establishes a raw L4 connection applying active proxy, DNS, and anti-DPI configurations.
func (c *Client) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := newFastDialer(&c.config)
	return dialer.DialContext(ctx, network, addr)
}

// DialTLSContext establishes an encrypted TLS socket connection using uTLS ClientHello specifications.
func (c *Client) DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := newFastDialer(&c.config)
	return dialer.DialTLSContext(ctx, network, addr)
}

// DialPlainForWS satisfies [aoni.WSDialer] by establishing a plain TCP socket for WebSocket upgrades.
func (c *Client) DialPlainForWS(ctx context.Context, addr string) (net.Conn, error) {
	conn, err := c.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	return c.applyWSFragmentation(ctx, conn), nil
}

// DialTLSForWS satisfies [aoni.WSDialer] by establishing an encrypted TLS connection for WebSocket upgrades.
func (c *Client) DialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	return c.DialTLSContext(ctx, "tcp", addr)
}

func (c *Client) applyWSFragmentation(ctx context.Context, conn net.Conn) net.Conn {
	if cfg := aoni.GetRequestConfig(ctx); cfg != nil && cfg.Fragment != nil {
		return fragment.NewFragmentedConn(conn, cfg.Fragment)
	}

	if c.config.Network.FragmentConfig != nil {
		return fragment.NewFragmentedConn(conn, c.config.Network.FragmentConfig)
	}

	return conn
}

var (
	_ aoni.RequestDoer = (*Client)(nil)
	_ aoni.WSDialer    = (*Client)(nil)
)

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

func (c *Client) dispatchPipeline(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
	reqAdapter *Request,
) (trailers map[string][]string, uncompressed bool, err error, autoReleased bool) {
	hasTelemetry := c.config.Defaults.Inspector != nil || c.config.Defaults.Pipeline.HAR != nil

	var startTime time.Time
	if hasTelemetry {
		startTime = time.Now()
	}

	if reqAdapter != nil {
		c.applyDefaultHeaders(reqAdapter)
		c.applyModifiers(reqAdapter, nil)
	}

	if c.config.Defaults.Pipeline.ProxyFailover == nil &&
		c.config.Network.HedgingDelay <= 0 &&
		c.config.Defaults.Pipeline.Hedging == nil {
		trailers, err, autoReleased = c.executeWithRedirects(ctx, fastReq, fastResp, reqAdapter)
	} else {
		trailers, err, autoReleased = c.executeWithHedging(ctx, fastReq, fastResp, reqAdapter)
	}

	if err != nil {
		if hasTelemetry {
			c.recordTelemetry(ctx, fastReq, nil, err, startTime)
		}

		return nil, false, err, autoReleased
	}

	uncompressed = decompressFastResponse(fastResp)

	if valErr := c.validateResponse(fastReq, fastResp); valErr != nil {
		if hasTelemetry {
			c.recordTelemetry(ctx, fastReq, fastResp, valErr, startTime)
		}

		return nil, false, valErr, false
	}

	if wafErr := c.handleWAFChallenge(ctx, fastReq, fastResp); wafErr != nil {
		if hasTelemetry {
			c.recordTelemetry(ctx, fastReq, fastResp, wafErr, startTime)
		}

		return nil, false, wafErr, false
	}

	if hasTelemetry {
		c.recordTelemetry(ctx, fastReq, fastResp, nil, startTime)
	}

	return trailers, uncompressed, nil, false
}

func decompressFastResponse(resp *fasthttp.Response) bool {
	encodingBytes := peekHeaderCaseInsensitiveFast(resp, "Content-Encoding")
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
	case strings.Contains(encoding, "zstd"):
		decompressed, err = fastZstdDecoder.DecodeAll(body, make([]byte, 0, len(body)*2))

	case strings.Contains(encoding, "br"):
		brReader := brotli.NewReader(bytes.NewReader(body))
		decompressed, err = io.ReadAll(brReader)

	case strings.Contains(encoding, "gzip"), strings.Contains(encoding, "deflate"):
		gzReader, gzErr := gzip.NewReader(bytes.NewReader(body))
		if gzErr == nil {
			decompressed, err = io.ReadAll(gzReader)
			_ = gzReader.Close()
		} else {
			err = gzErr
		}
	}

	if err == nil && len(decompressed) > 0 {
		resp.SetBody(decompressed)
		deleteHeaderCaseInsensitiveFast(resp, "Content-Encoding")
		deleteHeaderCaseInsensitiveFast(resp, "Content-Length")

		return true
	}

	return false
}

func peekHeaderCaseInsensitiveFast(resp *fasthttp.Response, key string) []byte {
	var found []byte
	resp.Header.All()(func(k, v []byte) bool {
		if bytesconv.EqualFoldASCII(bytesconv.B2S(k), key) {
			found = v
			return false
		}

		return true
	})

	return found
}

func deleteHeaderCaseInsensitiveFast(resp *fasthttp.Response, key string) {
	resp.Header.Del(key)
	resp.Header.Del(strings.ToLower(key))
}

func (c *Client) recordTelemetry(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
	execErr error,
	startTime time.Time,
) {
	inspector := c.config.Defaults.Inspector
	harTracker := c.config.Defaults.Pipeline.HAR

	if inspector == nil && harTracker == nil {
		return
	}

	var httpResp *http.Response
	if fastResp != nil {
		httpResp = toHTTPResponse(fastReq, fastResp) //nolint:bodyclose
	} else {
		reqMethod := string(fastReq.Header.Method())
		reqURI := string(fastReq.URI().FullURI())
		stdReq, _ := http.NewRequestWithContext(ctx, reqMethod, reqURI, nil)
		httpResp = &http.Response{Request: stdReq}
	}

	if inspector != nil {
		inspector.Capture(httpResp.Request, httpResp, execErr, nil)
	}

	if harTracker != nil && harTracker.Tracker != nil {
		duration := time.Since(startTime).Milliseconds()
		harTracker.Tracker.Record(httpResp.Request, httpResp, startTime, duration)
	}
}

type hedgeResult struct {
	resp     *fasthttp.Response
	trailers map[string][]string
	err      error
}

func (c *Client) executeWithHedging(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
	reqAdapter *Request,
) (trailers map[string][]string, err error, autoReleased bool) {
	delay := c.config.Network.HedgingDelay
	if c.config.Defaults.Pipeline.Hedging != nil && c.config.Defaults.Pipeline.Hedging.DefaultDelay > 0 {
		delay = c.config.Defaults.Pipeline.Hedging.DefaultDelay
	}

	if delay <= 0 {
		return c.executeWithProxyFailover(ctx, fastReq, fastResp, reqAdapter)
	}

	resultsCh := make(chan hedgeResult, 2)

	hedgeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	c.launchHedgedAttempt(hedgeCtx, fastReq, reqAdapter, resultsCh)

	timer := time.NewTimer(delay)
	defer timer.Stop()

	var req2Started bool

	select {
	case res := <-resultsCh:
		if res.err == nil {
			res.resp.CopyTo(fastResp)
			fasthttp.ReleaseResponse(res.resp)

			return res.trailers, nil, false
		}

		fasthttp.ReleaseResponse(res.resp)

		if !req2Started {
			req2Started = true //nolint:ineffassign

			c.launchHedgedAttempt(hedgeCtx, fastReq, reqAdapter, resultsCh)
		}

	case <-timer.C:
		req2Started = true //nolint:ineffassign

		c.launchHedgedAttempt(hedgeCtx, fastReq, reqAdapter, resultsCh)
	}

	for range 2 {
		res := <-resultsCh
		if res.err == nil {
			cancel()
			res.resp.CopyTo(fastResp)
			fasthttp.ReleaseResponse(res.resp)

			return res.trailers, nil, false
		}

		fasthttp.ReleaseResponse(res.resp)
	}

	return nil, ErrHedgingFailed, false
}

func (c *Client) launchHedgedAttempt(
	ctx context.Context,
	origReq *fasthttp.Request,
	reqAdapter *Request,
	resultsCh chan<- hedgeResult,
) {
	reqCopy := fasthttp.AcquireRequest()
	origReq.CopyTo(reqCopy)

	if reqAdapter != nil {
		if bodyCloser, err := reqAdapter.GetBody(); err == nil && bodyCloser != nil {
			reqCopy.SetBodyStream(bodyCloser, -1)
		}
	}

	go func() {
		defer fasthttp.ReleaseRequest(reqCopy)

		resp := fasthttp.AcquireResponse()
		trailers, err, _ := c.executeWithProxyFailover(ctx, reqCopy, resp, reqAdapter)

		resultsCh <- hedgeResult{resp: resp, trailers: trailers, err: err}
	}()
}

func (c *Client) applyCookies(ctx context.Context, req *fasthttp.Request) {
	jar := c.config.Engine.CookieJar
	if jar == nil {
		return
	}

	if pJar, ok := jar.(*cookie.ProxyIsolatedJar); ok {
		jar = pJar.GetJar(ctx)
	}

	if jar == nil {
		return
	}

	u, err := url.Parse(string(req.URI().FullURI()))
	if err != nil {
		return
	}

	cookies := jar.Cookies(u)
	if len(cookies) == 0 {
		return
	}

	var cookieHeader strings.Builder
	for i, c := range cookies {
		if i > 0 {
			cookieHeader.WriteString("; ")
		}

		cookieHeader.WriteString(c.Name)
		cookieHeader.WriteByte('=')
		cookieHeader.WriteString(c.Value)
	}

	if existing := req.Header.Peek("Cookie"); len(existing) > 0 {
		req.Header.Set("Cookie", string(existing)+"; "+cookieHeader.String())
	} else {
		req.Header.Set("Cookie", cookieHeader.String())
	}
}

func (c *Client) captureCookies(ctx context.Context, req *fasthttp.Request, resp *fasthttp.Response) {
	jar := c.config.Engine.CookieJar
	if jar == nil {
		return
	}

	if pJar, ok := jar.(*cookie.ProxyIsolatedJar); ok {
		jar = pJar.GetJar(ctx)
	}

	if jar == nil {
		return
	}

	u, err := url.Parse(string(req.URI().FullURI()))
	if err != nil {
		return
	}

	var cookies []*http.Cookie
	resp.Header.Cookies()(func(key, value []byte) bool {
		if cookie := parseCookie(key, value); cookie != nil {
			cookies = append(cookies, cookie)
		}

		return true
	})

	if len(cookies) > 0 {
		jar.SetCookies(u, cookies)
	}
}

func parseCookie(key, value []byte) *http.Cookie {
	header := http.Header{}
	header.Add("Set-Cookie", string(value))

	fakeResp := &http.Response{Header: header}

	parsed := fakeResp.Cookies()
	if len(parsed) > 0 {
		return parsed[0]
	}

	return nil
}

func (c *Client) executeWithProxyFailover(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
	reqAdapter *Request,
) (trailers map[string][]string, err error, autoReleased bool) {
	failover := c.config.Defaults.Pipeline.ProxyFailover
	if failover == nil || len(failover.Proxies) == 0 {
		return c.executeWithRedirects(ctx, fastReq, fastResp, reqAdapter)
	}

	maxRetries := failover.RetryLimit
	if maxRetries <= 0 {
		maxRetries = len(failover.Proxies)
	}

	for i := range maxRetries {
		proxyStr := failover.Proxies[i%len(failover.Proxies)]
		if u, parseErr := url.Parse(proxyStr); parseErr == nil {
			reqCfg := aoni.GetOrInitRequestConfig(ctx)
			reqCfg.ProxyAddr = u
		}

		trailers, err, autoReleased = c.executeWithRedirects(ctx, fastReq, fastResp, reqAdapter)
		if err == nil && !isProxyGatewayError(fastResp.StatusCode()) {
			return trailers, nil, autoReleased
		}

		if autoReleased {
			return nil, err, true
		}

		slurpAndResetResponse(fastResp)
	}

	return nil, fmt.Errorf("aoni fast: proxy failover exhausted retries: %w", err), false
}

func isProxyGatewayError(status int) bool {
	return status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout
}

func slurpAndResetResponse(resp *fasthttp.Response) {
	if resp == nil {
		return
	}

	if resp.IsBodyStream() {
		if stream := resp.BodyStream(); stream != nil {
			_, _ = io.CopyN(io.Discard, stream, maxBodySlurpBytes)
		}
	}

	resp.Reset()
}

func (c *Client) validateResponse(fastReq *fasthttp.Request, fastResp *fasthttp.Response) error {
	validator := c.config.Defaults.ResponseValidator
	if validator == nil {
		return nil
	}

	httpResp := toHTTPResponse(fastReq, fastResp) //nolint:bodyclose

	return validator(httpResp)
}

func (c *Client) handleWAFChallenge(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
) error {
	detector := c.config.Defaults.ChallengeDetector

	solver := c.config.Defaults.ChallengeSolver
	if detector == nil || solver == nil {
		return nil
	}

	httpResp := toHTTPResponse(fastReq, fastResp) //nolint:bodyclose

	isChallenge, err := detector(httpResp)
	if !isChallenge {
		return nil
	}

	solvedResp, solveErr := solver.Solve(ctx, err, httpResp.Request)
	if solveErr != nil {
		return solveErr
	}

	if solvedResp != nil && solvedResp.Body != nil {
		bodyBytes, _ := io.ReadAll(solvedResp.Body)
		_ = solvedResp.Body.Close()

		fastResp.SetBody(bodyBytes)
		fastResp.SetStatusCode(solvedResp.StatusCode)
	}

	return nil
}

func toHTTPResponse(fastReq *fasthttp.Request, fastResp *fasthttp.Response) *http.Response {
	reqMethod := string(fastReq.Header.Method())
	reqURI := string(fastReq.URI().FullURI())
	stdReq, _ := http.NewRequest(reqMethod, reqURI, nil) //nolint:noctx

	httpResp := &http.Response{
		StatusCode: fastResp.StatusCode(),
		Status:     http.StatusText(fastResp.StatusCode()),
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(fastResp.Body())),
		Request:    stdReq,
	}

	fastResp.Header.All()(func(k, v []byte) bool {
		httpResp.Header.Add(string(k), string(v))
		return true
	})

	return httpResp
}

func (c *Client) executeWithRedirects(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
	reqAdapter *Request,
) (trailers map[string][]string, err error, autoReleased bool) {
	redirectLimit := c.resolveRedirectLimit()
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
		if reqAdapter != nil && redirectsFollowed == 0 {
			c.applyModifiers(reqAdapter, nil)
		}

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

		if err := applyRedirectMethodAndBody(statusCode, fastReq, reqAdapter); err != nil {
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

			if len(fastReq.Header.Peek("sec-fetch-site")) > 0 {
				fastReq.Header.Set("sec-fetch-site", "cross-site")
			}
		} else if len(fastReq.Header.Peek("sec-fetch-site")) > 0 {
			fastReq.Header.Set("sec-fetch-site", "same-origin")
		}

		if isHTTPSDowngrade(currentURI, nextURI) {
			fastReq.Header.Del("Referer")
		} else {
			fastReq.Header.SetBytesK(bytesconv.S2B("Referer"), string(currentURI.FullURI()))
		}

		fasthttp.ReleaseURI(nextURI)
		slurpAndResetResponse(fastResp)
	}
}

func isSameHost(u1, u2 *fasthttp.URI) bool {
	h1 := strings.ToLower(netutil.CleanHost(string(u1.Host())))
	h2 := strings.ToLower(netutil.CleanHost(string(u2.Host())))
	return h1 == h2
}

func isHTTPSDowngrade(u1, u2 *fasthttp.URI) bool {
	return bytes.EqualFold(u1.Scheme(), []byte("https")) &&
		bytes.EqualFold(u2.Scheme(), []byte("http"))
}

func extractUserInfoAndSetAuth(req *fasthttp.Request) {
	fullURI := req.URI().FullURI()
	if bytes.IndexByte(fullURI, '@') == -1 {
		return
	}

	userInfo := req.URI().Username()
	if len(userInfo) == 0 {
		return
	}

	if len(req.Header.Peek("Authorization")) == 0 {
		user := string(req.URI().Username())
		pass := string(req.URI().Password())
		encoded := base64.StdEncoding.EncodeToString(bytesconv.S2B(user + ":" + pass))
		req.Header.Set("Authorization", "Basic "+encoded)
	}

	req.URI().SetUsername("")
}

type altSvcCache struct {
	mu sync.RWMutex

	_ cpu.CacheLinePad

	hosts     map[string]time.Time
	cooldowns map[string]time.Time

	_ cpu.CacheLinePad
}

var globalAltSvcCache = &altSvcCache{
	hosts:     make(map[string]time.Time),
	cooldowns: make(map[string]time.Time),
}

func (c *altSvcCache) MarkH3Failed(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cooldowns == nil {
		c.cooldowns = make(map[string]time.Time)
	}

	c.cooldowns[host] = time.Now().Add(5 * time.Minute)
}

func (c *altSvcCache) IsH3Supported(host string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.cooldowns != nil {
		if until, ok := c.cooldowns[host]; ok && time.Now().Before(until) {
			return false
		}
	}

	exp, ok := c.hosts[host]
	if !ok || time.Now().After(exp) {
		return false
	}

	return true
}

func (c *altSvcCache) Record(host, headerVal string) {
	if host == "" || headerVal == "" {
		return
	}

	if headerVal == "clear" {
		c.mu.Lock()
		delete(c.hosts, host)
		delete(c.cooldowns, host)
		c.mu.Unlock()

		return
	}

	if !strings.Contains(headerVal, "h3") {
		return
	}

	maxAge := parseMaxAge(headerVal)

	c.mu.Lock()
	c.hosts[host] = time.Now().Add(maxAge)
	c.mu.Unlock()
}

func parseMaxAge(headerVal string) time.Duration {
	maxAge := 86400 * time.Second

	for p := range strings.SplitSeq(headerVal, ";") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "ma=") {
			if seconds, err := strconv.ParseInt(p[3:], 10, 64); err == nil && seconds > 0 {
				return time.Duration(seconds) * time.Second
			}
		}
	}

	return maxAge
}

func (c *Client) dispatchSingleRequest(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	host := string(fastReq.URI().Host())
	alpnMode := resolveALPNMode(ctx, &c.config, fastReq)

	if alpnMode == aoni.AlpnH3 {
		h3 := c.getH3Client()

		tr, err := h3.Do(ctx, fastReq, fastResp, c.config.Fingerprint.HeaderOrder)
		if err == nil {
			return tr, nil, false
		}

		globalAltSvcCache.MarkH3Failed(host)
		fastResp.Reset()

		alpnMode = resolveALPNMode(ctx, &c.config, fastReq)
	}

	if alpnMode == aoni.AlpnH2 {
		h2Cl := c.getH2Client(host)

		tr, err := h2Cl.DoWithTrailers(ctx, fastReq, fastResp)
		if err == nil {
			if altSvc := fastResp.Header.Peek("Alt-Svc"); len(altSvc) > 0 {
				globalAltSvcCache.Record(host, string(altSvc))
			}

			return tr, nil, false
		}

		c.removeH2Client(host)
		fastResp.Reset()

		if c.config.Fingerprint.BrowserID != aoni.BrowserNone {
			freshH2Cl := c.getH2Client(host)

			trFresh, errFresh := freshH2Cl.DoWithTrailers(ctx, fastReq, fastResp)
			if errFresh == nil {
				if altSvc := fastResp.Header.Peek("Alt-Svc"); len(altSvc) > 0 {
					globalAltSvcCache.Record(host, string(altSvc))
				}

				return trFresh, nil, false
			}

			c.removeH2Client(host)
			fastResp.Reset()
		}
	}

	err, autoReleased = c.executeFastHTTP(ctx, fastReq, fastResp)
	if autoReleased {
		return nil, err, true
	}

	if err != nil && isH2FrameOnH1Error(err) {
		fastResp.Reset()

		h2Cl := c.getH2Client(host)

		tr, h2Err := h2Cl.DoWithTrailers(ctx, fastReq, fastResp)
		if h2Err == nil {
			if altSvc := fastResp.Header.Peek("Alt-Svc"); len(altSvc) > 0 {
				globalAltSvcCache.Record(host, string(altSvc))
			}

			return tr, nil, false
		}

		err = h2Err
	}

	if err == nil {
		if altSvc := fastResp.Header.Peek("Alt-Svc"); len(altSvc) > 0 {
			globalAltSvcCache.Record(host, string(altSvc))
		}
	}

	return nil, err, false
}

func isH2FrameOnH1Error(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	return strings.Contains(errStr, "reading response headers") ||
		strings.Contains(errStr, "\x00\x00\x12\x04") ||
		strings.Contains(errStr, "\x00\x00\x04")
}

func (c *Client) removeH2Client(host string) {
	c.h2Mutex.Lock()
	defer c.h2Mutex.Unlock()

	if c.h2Clients != nil {
		delete(c.h2Clients, host)
	}
}

func (c *Client) resolveRedirectLimit() int {
	if c.config.Engine.RedirectLimit < 0 {
		return 10
	}

	return c.config.Engine.RedirectLimit
}

func isRedirectStatus(code int) bool {
	return code == fasthttp.StatusMovedPermanently ||
		code == fasthttp.StatusFound ||
		code == fasthttp.StatusSeeOther ||
		code == fasthttp.StatusTemporaryRedirect ||
		code == fasthttp.StatusPermanentRedirect
}

func scrubSensitiveHeaders(req *fasthttp.Request, currentURI, nextURI *fasthttp.URI) {
	for _, h := range aoni.DefaultSensitiveHeaders {
		req.Header.Del(h)
	}

	req.Header.Del("Cookie2")
	req.Header.Del("Proxy-Authenticate")
	req.Header.Del("WWW-Authenticate")

	host1 := string(currentURI.Host())
	host2 := string(nextURI.Host())

	if !isSameDomainOrSubdomain(host1, host2) {
		req.Header.Del("Cookie")
	}
}

func isSameDomainOrSubdomain(h1, h2 string) bool {
	clean1 := strings.ToLower(netutil.CleanHost(h1))
	clean2 := strings.ToLower(netutil.CleanHost(h2))

	if clean1 == clean2 {
		return true
	}

	return strings.HasSuffix(clean1, "."+clean2) || strings.HasSuffix(clean2, "."+clean1)
}

func applyRedirectMethodAndBody(statusCode int, req *fasthttp.Request, reqAdapter *Request) error {
	switch statusCode {
	case fasthttp.StatusMovedPermanently, fasthttp.StatusFound, fasthttp.StatusSeeOther:
		method := string(req.Header.Method())
		if method != http.MethodGet && method != http.MethodHead {
			req.Header.SetMethod(http.MethodGet)
			req.SetBody(nil)
			req.Header.Del("Content-Type")
			req.Header.Del("Content-Length")
		}

	case fasthttp.StatusTemporaryRedirect, fasthttp.StatusPermanentRedirect:
		if req.IsBodyStream() {
			if reqAdapter == nil {
				return ErrCannotRewind
			}

			bodyCloser, err := reqAdapter.GetBody()
			if err != nil || bodyCloser == nil {
				return ErrCannotRewind
			}

			req.SetBodyStream(bodyCloser, -1)
		}
	}

	return nil
}

func (c *Client) getH3Client() *h3engine.Client {
	c.h3Once.Do(func() {
		tlsCfg := &tls.Config{
			InsecureSkipVerify: c.config.Engine.InsecureSkipVerify, //nolint:gosec
		}

		if spec := c.config.Fingerprint.TLSQUICClientHelloSpec; spec != nil && len(spec.CipherSuites) > 0 {
			tlsCfg.CipherSuites = spec.CipherSuites
		}

		quicCfg := &quic.Config{
			EnableDatagrams: true,
		}

		if h3s := c.config.Fingerprint.H3Settings; h3s != nil {
			quicCfg.InitialStreamReceiveWindow = h3s.InitialStreamReceiveWindow
			quicCfg.MaxStreamReceiveWindow = h3s.MaxStreamReceiveWindow
			quicCfg.InitialConnectionReceiveWindow = h3s.InitialConnectionReceiveWindow
			quicCfg.MaxConnectionReceiveWindow = h3s.MaxConnectionReceiveWindow
			quicCfg.MaxIncomingStreams = h3s.MaxIncomingStreams
			quicCfg.MaxIncomingUniStreams = h3s.MaxIncomingUniStreams
			quicCfg.EnableDatagrams = h3s.EnableDatagrams
		}

		c.h3Client = h3engine.NewClient(tlsCfg, quicCfg)
	})

	return c.h3Client
}

func (c *Client) getH2Client(host string) *h2engine.Client {
	c.h2Mutex.Lock()
	defer c.h2Mutex.Unlock()

	if c.h2Clients == nil {
		c.h2Clients = make(map[string]*h2engine.Client)
	}

	if cl, ok := c.h2Clients[host]; ok {
		return cl
	}

	dialer := &h2engine.Dialer{
		Addr: host,
		RawDialContext: func(ctx context.Context, addr string) (net.Conn, error) {
			fastD := newFastDialer(&c.config)
			return fastD.DialH2(ctx, addr)
		},
	}

	var h2s *h2engine.Settings
	if c.config.Fingerprint.H2Settings != nil {
		s := c.config.Fingerprint.H2Settings
		h2s = &h2engine.Settings{}
		h2s.SetHeaderTableSize(s.HeaderTableSize)
		h2s.SetPush(s.EnablePush == 1)
		h2s.SetMaxConcurrentStreams(s.MaxConcurrentStreams)
		h2s.SetMaxWindowSize(s.InitialWindowSize)
		h2s.SetMaxFrameSize(s.MaxFrameSize)
		h2s.SetMaxHeaderListSize(s.MaxHeaderListSize)
	}

	cl := h2engine.NewClient(dialer, h2engine.ClientOpts{
		PingInterval: 15 * time.Second,
		Settings:     h2s,
	})

	if len(c.config.Fingerprint.HeaderOrder) > 0 {
		cl.SetOrderedHeaders(c.config.Fingerprint.HeaderOrder)
	}

	c.h2Clients[host] = cl

	return cl
}

func resolveALPNMode(ctx context.Context, cfg *aoni.Config, fastReq *fasthttp.Request) string {
	reqCfg := aoni.GetRequestConfig(ctx)
	if reqCfg != nil {
		if len(reqCfg.Modifiers) > 0 && len(reqCfg.ALPNOverride) == 0 {
			dummyReq := NewRequest(fastReq)
			dummyReq.SetContext(ctx) // Attach context so modifiers mutate reqCfg

			for _, m := range reqCfg.Modifiers {
				if m != nil {
					m(dummyReq)
				}
			}
		}

		if len(reqCfg.ALPNOverride) > 0 {
			first := reqCfg.ALPNOverride[0]
			if first == aoni.AlpnH3 || first == aoni.AlpnH2 || first == aoni.AlpnHTTP {
				return first
			}
		}
	}

	if bytes.EqualFold(fastReq.URI().Scheme(), []byte("https")) {
		host := string(fastReq.URI().Host())
		if host != "" && globalAltSvcCache.IsH3Supported(host) {
			return aoni.AlpnH3
		}

		return aoni.AlpnH2
	}

	if cfg != nil {
		if len(cfg.Fingerprint.HeaderOrder) > 0 ||
			cfg.Fingerprint.H2Settings != nil ||
			cfg.Fingerprint.BrowserID != aoni.BrowserNone {
			return aoni.AlpnH2
		}
	}

	return aoni.AlpnHTTP
}

func (c *Client) applyEngineConfig() {
	if c.config.Engine.Timeout > 0 {
		c.engine.ReadTimeout = c.config.Engine.Timeout
		c.engine.WriteTimeout = c.config.Engine.Timeout
	}

	if c.config.Engine.InsecureSkipVerify {
		c.engine.TLSConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}

	c.engine.DisableHeaderNamesNormalizing = true
}

func (c *Client) applyCustomDialer() {
	dialer := newFastDialer(&c.config)
	c.engine.Dial = dialer.Dial
	c.engine.DialDualStack = true
}

func (c *Client) applyDefaultHeaders(req aoni.Request) {
	if req.Header("Accept-Encoding") == "" {
		req.SetHeader("Accept-Encoding", "zstd, br, gzip")
	}

	if len(c.config.Defaults.Headers) == 0 {
		return
	}

	for k, vv := range c.config.Defaults.Headers {
		if req.Header(k) == "" && len(vv) > 0 {
			req.SetHeader(k, vv[0])
		}
	}
}

func (c *Client) applyModifiers(req aoni.Request, mods []aoni.RequestModifier) {
	for _, defaultMod := range c.config.Defaults.DefaultMods {
		if defaultMod != nil {
			defaultMod(req)
		}
	}

	for _, m := range mods {
		if m != nil {
			m(req)
		}
	}
}

func (c *Client) executeFastHTTP(
	ctx context.Context,
	req *fasthttp.Request,
	resp *fasthttp.Response,
) (err error, autoReleased bool) {
	if err := ctx.Err(); err != nil {
		return err, false
	}

	// Prevent double-TLS while ensuring fasthttp dials port 443
	isHTTPS := bytes.EqualFold(req.URI().Scheme(), []byte("https"))
	origHost := req.URI().Host()
	hasHostHeader := len(req.Header.Peek("Host")) > 0

	if isHTTPS {
		hostStr := string(origHost)
		if !hasHostHeader {
			req.Header.SetHostBytes(origHost)
		}

		if !strings.Contains(hostStr, ":") {
			req.URI().SetHost(hostStr + ":443")
		}

		req.URI().SetScheme("http")
	}

	defer func() {
		if isHTTPS {
			req.URI().SetScheme("https")
			req.URI().SetHostBytes(origHost)

			if !hasHostHeader {
				req.Header.Del("Host")
			}
		}
	}()

	var proxyURL *url.URL
	if c.config.Network.ProxyAddr != nil {
		proxyURL = c.config.Network.ProxyAddr
	}

	if reqCfg := aoni.GetRequestConfig(ctx); reqCfg != nil && reqCfg.ProxyAddr != nil {
		proxyURL = reqCfg.ProxyAddr
	}

	if rawProxy, ok := aoni.GetProxyOverride(ctx).Value(); ok && rawProxy != "" {
		if parsed, parseErr := url.Parse(rawProxy); parseErr == nil {
			proxyURL = parsed
		}
	}

	if proxyURL != nil && (proxyURL.Scheme == "http" || proxyURL.Scheme == "https") && !isHTTPS {
		req.UseHostHeader = true
		req.Header.SetRequestURIBytes(req.URI().FullURI())
	}

	// Fast path: if context is standard Background/TODO without active cancellation, execute synchronously!
	if ctx == context.Background() || ctx == context.TODO() || ctx.Done() == nil {
		var err error
		if deadline, ok := ctx.Deadline(); ok {
			err = c.engine.DoDeadline(req, resp, deadline)
		} else if c.config.Engine.Timeout > 0 {
			err = c.engine.DoTimeout(req, resp, c.config.Engine.Timeout)
		} else {
			err = c.engine.Do(req, resp)
		}

		if err != nil && isStaleKeepAliveError(err) {
			fastRespReset(resp)

			if isHTTPS {
				req.URI().SetScheme("https")
				req.URI().SetHostBytes(origHost)
			}

			req.SetConnectionClose()

			return c.engine.Do(req, resp), false
		}

		return err, false
	}

	// Slow path: context carries active cancellation (channel + watcher goroutine)
	done := make(chan error, 1)

	go func() {
		if deadline, ok := ctx.Deadline(); ok {
			done <- c.engine.DoDeadline(req, resp, deadline)
			return
		}

		if c.config.Engine.Timeout > 0 {
			done <- c.engine.DoTimeout(req, resp, c.config.Engine.Timeout)
			return
		}

		done <- c.engine.Do(req, resp)
	}()

	select {
	case <-ctx.Done():
		go func() {
			<-done

			if isHTTPS {
				req.URI().SetScheme("https")
				req.URI().SetHostBytes(origHost)

				if !hasHostHeader {
					req.Header.Del("Host")
				}
			}

			fasthttp.ReleaseRequest(req)
			fasthttp.ReleaseResponse(resp)
		}()

		return ctx.Err(), true

	case err := <-done:
		if err != nil && isStaleKeepAliveError(err) {
			fastRespReset(resp)

			if isHTTPS {
				req.URI().SetScheme("https")
				req.URI().SetHostBytes(origHost)
			}

			req.SetConnectionClose()

			return c.engine.Do(req, resp), false
		}

		return err, false
	}
}

func isStaleKeepAliveError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	return strings.Contains(errStr, "connection closed") ||
		strings.Contains(errStr, "closed connection") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "eof") ||
		strings.Contains(errStr, "reading response headers") ||
		strings.Contains(errStr, "use of closed") ||
		strings.Contains(errStr, "reset by peer")
}

func fastRespReset(resp *fasthttp.Response) {
	if resp != nil {
		resp.Reset()
	}
}

func (c *Client) resolveTargetURL(req aoni.Request, path string) error {
	if len(path) >= 7 && (strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")) {
		req.SetURL(path)
		return nil
	}

	if c.config.Defaults.BaseURL != nil && c.config.Defaults.BaseURL.Host != "" {
		base := c.config.Defaults.BaseURL
		basePath := strings.TrimSuffix(base.Path, "/")

		cleanPath := path
		if cleanPath != "" && cleanPath[0] != '/' {
			cleanPath = "/" + cleanPath
		}

		fullURL := base.Scheme + "://" + base.Host + basePath + cleanPath
		req.SetURL(fullURL)

		return nil
	}

	if path == "" {
		return ErrTargetURLEmpty
	}

	req.SetURL(path)

	return nil
}
