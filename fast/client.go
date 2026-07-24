// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fast provides high-performance fasthttp engine adapters for [aoni.Request] and [aoni.Response].
package fast

import (
	"bytes"
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

	"github.com/quic-go/quic-go"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast/h2engine"
	"github.com/lemon4ksan/aoni/fast/h3engine"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
	"github.com/lemon4ksan/aoni/netutil"
)

// Client executes ultra-high-performance HTTP requests over fasthttp,
// seamlessly multiplexing native H1 (fasthttp), native H2 (h2engine), and native H3 (h3engine).
type Client struct {
	engine    *fasthttp.Client
	h2Clients map[string]*h2engine.Client
	h3Client  *h3engine.Client
	config    aoni.Config
	h2Mutex   sync.Mutex
	h3Once    sync.Once
}

// Option is an alias for [aoni.ClientOption].
type Option = aoni.ClientOption

// NewClient creates a new multiprotocol Client configured with fasthttp, uTLS,
// native HTTP/2 framing, and native HTTP/3 QUIC support.
func NewClient(opts ...aoni.ClientOption) *Client {
	c := &Client{
		engine: &fasthttp.Client{
			ReadTimeout:         15 * time.Second,
			WriteTimeout:        15 * time.Second,
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
	fastReq := fasthttp.AcquireRequest()
	fastResp := fasthttp.AcquireResponse()

	reqAdapter := NewRequest(fastReq)
	reqAdapter.SetContext(ctx)
	reqAdapter.SetMethod(method)

	if err := c.resolveTargetURL(reqAdapter, path); err != nil {
		fasthttp.ReleaseRequest(fastReq)
		fasthttp.ReleaseResponse(fastResp)

		return nil, err
	}

	c.applyDefaultHeaders(reqAdapter)
	c.applyModifiers(reqAdapter, mods)

	isAutoAE := len(fastReq.Header.Peek("Accept-Encoding")) == 0
	c.applyDefaultHeaders(reqAdapter)
	c.applyModifiers(reqAdapter, mods)

	reqCtx := reqAdapter.Context()

	trailers, err, autoReleased := c.dispatchPipeline(reqCtx, fastReq, fastResp, reqAdapter)
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

	if isAutoAE && len(fastResp.Header.Peek("Content-Encoding")) > 0 {
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

	fastResp := fasthttp.AcquireResponse()

	trailers, err, autoReleased := c.dispatchPipeline(ctx, fastReq, fastResp, reqAdapter)
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

	return respAdapter, nil
}

func (c *Client) dispatchPipeline(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
	reqAdapter *Request,
) (trailers map[string][]string, err error, autoReleased bool) {
	startTime := time.Now()

	c.applyCookies(fastReq)

	trailers, err, autoReleased = c.executeWithHedging(ctx, fastReq, fastResp, reqAdapter)
	if err != nil {
		c.recordTelemetry(ctx, fastReq, nil, err, startTime)
		return nil, err, autoReleased
	}

	c.captureCookies(fastReq, fastResp)

	if valErr := c.validateResponse(fastReq, fastResp); valErr != nil {
		c.recordTelemetry(ctx, fastReq, fastResp, valErr, startTime)
		return nil, valErr, false
	}

	if wafErr := c.handleWAFChallenge(ctx, fastReq, fastResp); wafErr != nil {
		c.recordTelemetry(ctx, fastReq, fastResp, wafErr, startTime)
		return nil, wafErr, false
	}

	c.recordTelemetry(ctx, fastReq, fastResp, nil, startTime)

	return trailers, nil, false
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
		httpResp = toHTTPResponse(fastReq, fastResp)
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
			req2Started = true
			c.launchHedgedAttempt(hedgeCtx, fastReq, reqAdapter, resultsCh)
		}

	case <-timer.C:
		req2Started = true
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

func (c *Client) applyCookies(req *fasthttp.Request) {
	jar := c.config.Engine.CookieJar
	if jar == nil {
		return
	}

	u, err := url.Parse(string(req.URI().FullURI()))
	if err != nil {
		return
	}

	cookies := jar.Cookies(u)
	for _, cookie := range cookies {
		req.Header.SetCookie(cookie.Name, cookie.Value)
	}
}

func (c *Client) captureCookies(req *fasthttp.Request, resp *fasthttp.Response) {
	jar := c.config.Engine.CookieJar
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
	header.Add("Set-Cookie", string(key)+"="+string(value))

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

	httpResp := toHTTPResponse(fastReq, fastResp)

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

	httpResp := toHTTPResponse(fastReq, fastResp)
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
	stdReq, _ := http.NewRequest(reqMethod, reqURI, nil)

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
		extractUserInfoAndSetAuth(fastReq)
		return c.dispatchSingleRequest(ctx, fastReq, fastResp)
	}

	currentURI := fasthttp.AcquireURI()
	defer fasthttp.ReleaseURI(currentURI)

	var redirectsFollowed int

	for {
		fastReq.URI().CopyTo(currentURI)
		extractUserInfoAndSetAuth(fastReq)

		trailers, err, autoReleased = c.dispatchSingleRequest(ctx, fastReq, fastResp)
		if err != nil {
			return nil, err, autoReleased
		}

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
		fastReq.URI().UpdateBytes(location)
		fastReq.URI().CopyTo(nextURI)

		if isCrossOriginURI(currentURI, nextURI) {
			scrubSensitiveHeaders(fastReq, currentURI, nextURI)
		}

		if isHTTPSDowngrade(currentURI, nextURI) {
			fastReq.Header.Del("Referer")
		}

		fasthttp.ReleaseURI(nextURI)
		slurpAndResetResponse(fastResp)
	}
}

func isHTTPSDowngrade(u1, u2 *fasthttp.URI) bool {
	return bytes.EqualFold(u1.Scheme(), []byte("https")) &&
		bytes.EqualFold(u2.Scheme(), []byte("http"))
}

func extractUserInfoAndSetAuth(req *fasthttp.Request) {
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
	mu        sync.RWMutex
	hosts     map[string]time.Time
	cooldowns map[string]time.Time
}

var globalAltSvcCache = &altSvcCache{
	hosts:     make(map[string]time.Time),
	cooldowns: make(map[string]time.Time),
}

// MarkH3Failed puts host in a temporary HTTP/3 cooldown upon QUIC dial or network errors.
func (c *altSvcCache) MarkH3Failed(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cooldowns == nil {
		c.cooldowns = make(map[string]time.Time)
	}

	// Disable H3 attempts for this host for 5 minutes after a QUIC/UDP block
	c.cooldowns[host] = time.Now().Add(5 * time.Minute)
}

// IsH3Supported reports whether HTTP/3 is supported for host and not currently in cooldown.
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
	parts := strings.Split(headerVal, ";")

	for _, p := range parts {
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
	alpnMode := resolveALPNMode(ctx, &c.config, host)

	if alpnMode == aoni.AlpnH3 {
		h3 := c.getH3Client()
		tr, err := h3.Do(ctx, fastReq, fastResp, c.config.Fingerprint.HeaderOrder)

		if err == nil {
			return tr, nil, false
		}

		// QUIC/UDP network block or handshake failure: fallback to H2/H1 transparently
		globalAltSvcCache.MarkH3Failed(host)
		fastResp.Reset()

		alpnMode = resolveALPNMode(ctx, &c.config, host)
	}

	if alpnMode == aoni.AlpnH2 {
		h2Cl := c.getH2Client(host)
		tr, err := h2Cl.DoWithTrailers(ctx, fastReq, fastResp)

		if altSvc := fastResp.Header.Peek("Alt-Svc"); len(altSvc) > 0 {
			globalAltSvcCache.Record(host, string(altSvc))
		}

		return tr, err, false
	}

	err, autoReleased = c.executeFastHTTP(ctx, fastReq, fastResp)

	if altSvc := fastResp.Header.Peek("Alt-Svc"); len(altSvc) > 0 {
		globalAltSvcCache.Record(host, string(altSvc))
	}

	return nil, err, autoReleased
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

func isCrossOriginURI(u1, u2 *fasthttp.URI) bool {
	return !bytes.EqualFold(u1.Scheme(), u2.Scheme()) ||
		!bytes.EqualFold(u1.Host(), u2.Host())
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

	// If cross-domain (not the same domain or parent/subdomain relationship), force-delete manual Cookie headers
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
			InsecureSkipVerify: c.config.Engine.InsecureSkipVerify,
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
		RawDial: func(addr string) (net.Conn, error) {
			fastD := newFastDialer(&c.config)
			return fastD.DialH2(context.Background(), addr)
		},
	}

	cl := h2engine.NewClient(dialer, h2engine.ClientOpts{
		PingInterval: 15 * time.Second,
	})

	if len(c.config.Fingerprint.HeaderOrder) > 0 {
		cl.SetOrderedHeaders(c.config.Fingerprint.HeaderOrder)
	}

	c.h2Clients[host] = cl

	return cl
}

func resolveALPNMode(ctx context.Context, cfg *aoni.Config, host string) string {
	reqCfg := aoni.GetRequestConfig(ctx)
	if reqCfg != nil && len(reqCfg.ALPNOverride) > 0 {
		first := reqCfg.ALPNOverride[0]
		if first == aoni.AlpnH3 || first == aoni.AlpnH2 {
			return first
		}
	}

	if globalAltSvcCache.IsH3Supported(host) {
		return aoni.AlpnH3
	}

	if len(cfg.Fingerprint.HeaderOrder) > 0 && slicesContains(cfg.Fingerprint.HeaderOrder, ":method") {
		return aoni.AlpnH2
	}

	return aoni.AlpnHTTP
}

func slicesContains(slice []string, target string) bool {
	for _, item := range slice {
		if item == target {
			return true
		}
	}

	return false
}

func (c *Client) applyEngineConfig() {
	if c.config.Engine.Timeout > 0 {
		c.engine.ReadTimeout = c.config.Engine.Timeout
		c.engine.WriteTimeout = c.config.Engine.Timeout
	}

	if c.config.Engine.InsecureSkipVerify {
		c.engine.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}

	c.engine.DisableHeaderNamesNormalizing = true
}

func (c *Client) applyCustomDialer() {
	dialer := newFastDialer(&c.config)
	c.engine.Dial = dialer.Dial
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

// executeFastHTTP executes an H1 request via fasthttp, ensuring memory safety
// and context cancellation without pool corruption or data races.
//
// Postconditions:
//   - If autoReleased is true, ownership of fastReq/fastResp is retained by
//     the background goroutine; caller MUST NOT release or touch them.
func (c *Client) executeFastHTTP(
	ctx context.Context,
	req *fasthttp.Request,
	resp *fasthttp.Response,
) (err error, autoReleased bool) {
	if err := ctx.Err(); err != nil {
		return err, false
	}

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
			fasthttp.ReleaseRequest(req)
			fasthttp.ReleaseResponse(resp)
		}()

		return ctx.Err(), true

	case err := <-done:
		return err, false
	}
}

func (c *Client) resolveTargetURL(req aoni.Request, path string) error {
	baseURLStr := c.config.Defaults.BaseURL.String()

	if path == "" && baseURLStr == "" {
		return ErrTargetURLEmpty
	}

	if len(path) >= 7 && (strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")) {
		req.SetURL(path)
		return nil
	}

	if baseURLStr == "" {
		req.SetURL(path)
		return nil
	}

	baseURL := strings.TrimSuffix(baseURLStr, "/")
	cleanPath := path

	if len(path) == 0 || path[0] != '/' {
		cleanPath = "/" + path
	}

	req.SetURL(baseURL + cleanPath)

	return nil
}

// PooledResponse wraps a fasthttp response and returns instances back to [sync.Pool] upon Close.
type PooledResponse struct {
	*Response
	fastReq  *fasthttp.Request
	fastResp *fasthttp.Response
	once     sync.Once
}

// NewPooledResponse creates a PooledResponse adapter around fastResp.
func NewPooledResponse(fastReq *fasthttp.Request, fastResp *fasthttp.Response) *PooledResponse {
	return &PooledResponse{
		Response: NewResponse(fastResp),
		fastReq:  fastReq,
		fastResp: fastResp,
	}
}

// Close releases underlying fasthttp request and response objects back to [sync.Pool].
func (r *PooledResponse) Close() error {
	r.once.Do(func() {
		if r.fastReq != nil {
			fasthttp.ReleaseRequest(r.fastReq)
			r.fastReq = nil
		}

		if r.fastResp != nil {
			fasthttp.ReleaseResponse(r.fastResp)
			r.fastResp = nil
		}
	})

	return nil
}

var (
	_ aoni.Response    = (*PooledResponse)(nil)
	_ aoni.RequestDoer = (*Client)(nil)
)
