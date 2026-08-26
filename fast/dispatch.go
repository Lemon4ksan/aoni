// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	fheader "github.com/lemon4ksan/foundation/net/http/header"
	furl "github.com/lemon4ksan/foundation/net/url"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/pool"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/aoni/internal/pipeline"
)

// dispatchSingleRequest routes an HTTP request through Happy Eyeballs v3 (Protocol Racing),
// racing HTTP/3 (QUIC) against HTTP/2/HTTP/1 (TCP/TLS) with a staggered fallback timer.
func (c *Client) dispatchSingleRequest(
	ctx context.Context,
	fastReq *h1engine.Request,
	fastResp *h1engine.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	sanitizeTraceHeaders(fastReq)

	// Enforce RFC 8470 / RFC 9001: 0-RTT Early Data is strictly restricted to safe idempotent methods.
	method := bytesconv.B2S(fastReq.Header.Method())
	if method != h1engine.MethodGet && method != h1engine.MethodHead && method != h1engine.MethodOptions {
		reqCfg := pipeline.GetOrInitRequestConfig(ctx)
		reqCfg.Disable0RTT = true
		fastReq.Header.Del(h1engine.HeaderEarlyData)
	}

	host := bytesconv.B2S(fastReq.URI().Host())
	alpnMode := c.resolveALPNMode(ctx, fastReq)

	staggerDelay := c.cfg.Network.HappyEyeballsDelay
	if alpnMode == aoni.AlpnH3 && c.shouldRaceProtocols(ctx) {
		return c.raceProtocolHandshakes(ctx, host, fastReq, fastResp, staggerDelay)
	}

	if alpnMode == aoni.AlpnH3 {
		if tr, h3Err, handled := c.tryDispatchH3(ctx, host, fastReq, fastResp); handled {
			return tr, h3Err, false
		}

		alpnMode = c.resolveALPNMode(ctx, fastReq)
	}

	if alpnMode == aoni.AlpnH2 {
		if tr, h2Err, handled := c.tryDispatchH2(ctx, host, fastReq, fastResp); handled {
			return tr, h2Err, false
		}
	}

	return c.dispatchH1WithFallbacks(ctx, host, fastReq, fastResp)
}

func (c *Client) shouldRaceProtocols(ctx context.Context) bool {
	reqCfg := aoni.GetRequestConfig(ctx)
	if reqCfg != nil && reqCfg.DisableAltSvc {
		return false
	}

	return true
}

type raceResult struct {
	trailers     map[string][]string
	err          error
	autoReleased bool
	isH3         bool
	resp         *h1engine.Response
}

func (c *Client) raceProtocolHandshakes(
	ctx context.Context,
	host string,
	fastReq *h1engine.Request,
	fastResp *h1engine.Response,
	staggerDelay time.Duration,
) (map[string][]string, error, bool) {
	if staggerDelay <= 0 {
		staggerDelay = 250 * time.Millisecond
	}

	results := make(chan raceResult, 2)
	raceCtx, cancelRace := context.WithCancel(ctx)

	// Safe pool drainer preventing memory leak from late losing goroutine responses
	defer func() {
		cancelRace()

		go drainLateRaceResponses(results)
	}()

	go func() {
		h3Resp := h1engine.AcquireResponse()

		tr, h3Err, handled := c.tryDispatchH3(raceCtx, host, fastReq, h3Resp)
		if handled && h3Err == nil {
			results <- raceResult{trailers: tr, err: nil, isH3: true, resp: h3Resp}
			return
		}

		h1engine.ReleaseResponse(h3Resp)

		results <- raceResult{err: h3Err, isH3: true}
	}()

	staggerTimer := pool.AcquireTimer(staggerDelay)
	defer pool.ReleaseTimer(staggerTimer)

	var tcpStarted bool

	select {
	case res := <-results:
		if res.isH3 && res.err == nil {
			res.resp.CopyTo(fastResp)
			h1engine.ReleaseResponse(res.resp)
			return res.trailers, nil, false
		}

	case <-staggerTimer.C:
		tcpStarted = true

		go func() {
			tcpResp := h1engine.AcquireResponse()

			tr, tcpErr, released := c.dispatchH1OrH2(raceCtx, host, fastReq, tcpResp)
			if tcpErr == nil {
				results <- raceResult{trailers: tr, err: nil, autoReleased: released, isH3: false, resp: tcpResp}
				return
			}

			h1engine.ReleaseResponse(tcpResp)

			results <- raceResult{err: tcpErr, autoReleased: released, isH3: false}
		}()
	}

	if !tcpStarted {
		return c.dispatchH1OrH2(ctx, host, fastReq, fastResp)
	}

	var firstErr error
	for range 2 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err(), false
		case res := <-results:
			if res.err == nil && res.resp != nil {
				res.resp.CopyTo(fastResp)
				h1engine.ReleaseResponse(res.resp)
				return res.trailers, nil, res.autoReleased
			}

			if firstErr == nil {
				firstErr = res.err
			}
		}
	}

	return nil, firstErr, false
}

func drainLateRaceResponses(results chan raceResult) {
	timer := pool.AcquireTimer(2 * time.Second)
	defer pool.ReleaseTimer(timer)

	for range 2 {
		select {
		case res := <-results:
			if res.resp != nil {
				h1engine.ReleaseResponse(res.resp)
			}
		case <-timer.C:
			return
		}
	}
}

func (c *Client) dispatchH1OrH2(
	ctx context.Context,
	host string,
	fastReq *h1engine.Request,
	fastResp *h1engine.Response,
) (map[string][]string, error, bool) {
	alpnMode := c.resolveALPNMode(ctx, fastReq)
	if alpnMode == aoni.AlpnH2 {
		if tr, h2Err, handled := c.tryDispatchH2(ctx, host, fastReq, fastResp); handled {
			return tr, h2Err, false
		}
	}

	return c.dispatchH1WithFallbacks(ctx, host, fastReq, fastResp)
}

func (c *Client) tryDispatchH3(
	ctx context.Context,
	host string,
	fastReq *h1engine.Request,
	fastResp *h1engine.Response,
) (map[string][]string, error, bool) {
	h3 := c.getH3Client()

	tr, err := h3.Do(ctx, fastReq, fastResp, c.cfg.Fingerprint.HeaderOrder)
	if err != nil {
		if c.protocolState.altSvc != nil {
			c.protocolState.altSvc.MarkH3Failed(host)
		}

		fastResp.Reset()

		return nil, err, false
	}

	if c.isRecoverableStatus(fastResp.StatusCode()) {
		tr, errRec, _ := c.recoverSpecialStatus(ctx, fastReq, fastResp)
		return tr, errRec, true
	}

	if c.protocolState.altSvc != nil {
		c.protocolState.altSvc.MarkH3Success(host)
	}

	return tr, nil, true
}

func (c *Client) tryDispatchH2(
	ctx context.Context,
	host string,
	fastReq *h1engine.Request,
	fastResp *h1engine.Response,
) (map[string][]string, error, bool) {
	h2Cl := c.getH2Client(host)

	tr, err := h2Cl.DoWithTrailers(ctx, fastReq, fastResp)
	if err != nil && c.cfg.Fingerprint.BrowserID != aoni.BrowserNone {
		c.removeH2Client(host)
		fastResp.Reset()

		freshH2Cl := c.getH2Client(host)
		tr, err = freshH2Cl.DoWithTrailers(ctx, fastReq, fastResp)
	}

	if err != nil {
		c.removeH2Client(host)
		fastResp.Reset()

		return nil, err, false
	}

	if c.isRecoverableStatus(fastResp.StatusCode()) {
		c.removeH2Client(host)
		trRec, errRec, _ := c.recoverSpecialStatus(ctx, fastReq, fastResp)

		return trRec, errRec, true
	}

	c.recordAltSvcIfPresent(host, fastResp)

	return tr, nil, true
}

func (c *Client) dispatchH1WithFallbacks(
	ctx context.Context,
	host string,
	fastReq *h1engine.Request,
	fastResp *h1engine.Response,
) (map[string][]string, error, bool) {
	err, autoReleased := c.executeFastHTTP(ctx, fastReq, fastResp)
	if autoReleased {
		return nil, err, true
	}

	if err != nil && isH2FrameOnH1Error(err) {
		return c.fallbackH1ToH2(ctx, host, fastReq, fastResp)
	}

	if err != nil {
		return nil, err, false
	}

	if c.isRecoverableStatus(fastResp.StatusCode()) {
		tr, errRec, released := c.recoverSpecialStatus(ctx, fastReq, fastResp)
		return tr, errRec, released
	}

	c.recordAltSvcIfPresent(host, fastResp)

	return nil, nil, false
}

func (c *Client) fallbackH1ToH2(
	ctx context.Context,
	host string,
	fastReq *h1engine.Request,
	fastResp *h1engine.Response,
) (map[string][]string, error, bool) {
	fastResp.Reset()

	h2Cl := c.getH2Client(host)

	tr, err := h2Cl.DoWithTrailers(ctx, fastReq, fastResp)
	if err != nil {
		return nil, err, false
	}

	if c.isRecoverableStatus(fastResp.StatusCode()) {
		c.removeH2Client(host)
		trRec, errRec, released := c.recoverSpecialStatus(ctx, fastReq, fastResp)

		return trRec, errRec, released
	}

	c.recordAltSvcIfPresent(host, fastResp)

	return tr, nil, false
}

func (c *Client) isRecoverableStatus(code int) bool {
	return code == http.StatusMisdirectedRequest ||
		code == http.StatusRequestTimeout ||
		code == http.StatusTooEarly
}

func (c *Client) recoverSpecialStatus(
	ctx context.Context,
	fastReq *h1engine.Request,
	fastResp *h1engine.Response,
) (map[string][]string, error, bool) {
	code := fastResp.StatusCode()
	fastResp.Reset()

	switch code {
	case http.StatusTooEarly:
		return c.retry425TooEarly(ctx, fastReq, fastResp)
	case http.StatusMisdirectedRequest:
		return c.retry421Misdirected(ctx, fastReq, fastResp)
	default:
		return c.retry408Timeout(ctx, fastReq, fastResp)
	}
}

func (c *Client) retry425TooEarly(
	ctx context.Context,
	fastReq *h1engine.Request,
	fastResp *h1engine.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	reqCfg := pipeline.GetOrInitRequestConfig(ctx)
	reqCfg.Disable0RTT = true

	host := bytesconv.B2S(fastReq.URI().Host())
	c.removeH2Client(host)

	fastReq.Header.Del("Early-Data")

	return c.dispatchSingleRequest(ctx, fastReq, fastResp)
}

func (c *Client) retry421Misdirected(
	ctx context.Context,
	fastReq *h1engine.Request,
	fastResp *h1engine.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	reqCfg := pipeline.GetOrInitRequestConfig(ctx)
	reqCfg.DisableAltSvc = true

	host := bytesconv.B2S(fastReq.URI().Host())
	if c.protocolState.altSvc != nil {
		c.protocolState.altSvc.MarkH3Failed(host)
	}

	c.removeH2Client(host)

	fastReq.Header.Del("Alt-Svc")

	return c.dispatchSingleRequest(ctx, fastReq, fastResp)
}

func (c *Client) retry408Timeout(
	ctx context.Context,
	fastReq *h1engine.Request,
	fastResp *h1engine.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	c.removeH2Client(bytesconv.B2S(fastReq.URI().Host()))
	fastReq.SetConnectionClose()

	return c.dispatchSingleRequest(ctx, fastReq, fastResp)
}

func (c *Client) recordAltSvcIfPresent(host string, fastResp *h1engine.Response) {
	if c.protocolState.altSvc == nil {
		return
	}

	if altSvc := fastResp.Header.Peek("Alt-Svc"); len(altSvc) > 0 {
		c.protocolState.altSvc.Record(host, string(altSvc))
	}
}

func (c *Client) executeFastHTTP(
	ctx context.Context,
	req *h1engine.Request,
	resp *h1engine.Response,
) (err error, autoReleased bool) {
	if err := ctx.Err(); err != nil {
		return err, false
	}

	ensureConnectionTE(req)

	isHTTPS, origHost, hasHostHeader, cleanup := c.setupFastHTTPSchemeAndHost(req)
	defer cleanup()

	c.configureFastHTTPProxy(ctx, req, isHTTPS)

	restoreOriginalTarget := func() {
		if isHTTPS {
			req.URI().SetScheme("https")
			req.URI().SetHostBytes(origHost)
		}
	}

	if ctx == nil || ctx == context.Background() || ctx == context.TODO() || ctx.Done() == nil {
		exec := func() error { return c.doFastHTTPEngine(ctx, req, resp) }
		err = c.executeFastHTTPWithStaleRetry(req, resp, exec, restoreOriginalTarget)

		return err, false
	}

	done := make(chan error, 1)

	go func() {
		done <- c.doFastHTTPEngine(ctx, req, resp)
	}()

	select {
	case <-ctx.Done():
		go func() {
			<-done
			restoreOriginalTarget()

			if isHTTPS && !hasHostHeader {
				req.Header.Del(fheader.Host)
			}

			h1engine.ReleaseRequest(req)
			h1engine.ReleaseResponse(resp)
		}()

		return ctx.Err(), true

	case err := <-done:
		if err != nil && isStaleKeepAliveError(err) {
			fastRespReset(resp)
			restoreOriginalTarget()
			req.SetConnectionClose()

			return c.engine.Do(req, resp), false
		}

		return err, false
	}
}

func (c *Client) setupFastHTTPSchemeAndHost(
	req *h1engine.Request,
) (isHTTPS bool, origHost []byte, hasHostHeader bool, cleanup func()) {
	isHTTPS = bytes.EqualFold(req.URI().Scheme(), []byte("https"))
	origHost = req.URI().Host()
	hasHostHeader = len(req.Header.Peek(fheader.Host)) > 0

	var hostStr string

	if isHTTPS {
		hostStr = string(origHost)
		if !hasHostHeader {
			req.Header.SetHostBytes(origHost)
		}

		if !strings.Contains(hostStr, ":") {
			hostStr += ":443"
			req.URI().SetHost(hostStr)
		}

		c.TrackHTTPSTarget(hostStr)
		req.URI().SetScheme("http")
	}

	cleanup = func() {
		if isHTTPS {
			c.UntrackHTTPSTarget(hostStr)
			req.URI().SetScheme("https")
			req.URI().SetHostBytes(origHost)

			if !hasHostHeader {
				req.Header.Del(fheader.Host)
			}
		}
	}

	return isHTTPS, origHost, hasHostHeader, cleanup
}

func (c *Client) configureFastHTTPProxy(ctx context.Context, req *h1engine.Request, isHTTPS bool) {
	var proxyURL *url.URL
	if c.cfg.Network.ProxyAddr != nil {
		proxyURL = c.cfg.Network.ProxyAddr
	}

	if reqCfg := aoni.GetRequestConfig(ctx); reqCfg != nil && reqCfg.ProxyAddr != nil {
		proxyURL = reqCfg.ProxyAddr
	}

	if rawProxy, ok := aoni.GetProxyOverride(ctx).Value(); ok && rawProxy != "" {
		if parsed, parseErr := furl.Parse(rawProxy); parseErr == nil {
			proxyURL = parsed
		}
	}

	if proxyURL != nil && (proxyURL.Scheme == "http" || proxyURL.Scheme == "https") && !isHTTPS {
		req.UseHostHeader = true
		req.Header.SetRequestURIBytes(req.URI().FullURI())
	}
}

func (c *Client) doFastHTTPEngine(ctx context.Context, req *h1engine.Request, resp *h1engine.Response) error {
	if deadline, ok := ctx.Deadline(); ok {
		return c.engine.DoDeadline(req, resp, deadline)
	}

	if c.cfg.Engine.Timeout > 0 {
		return c.engine.DoTimeout(req, resp, c.cfg.Engine.Timeout)
	}

	return c.engine.Do(req, resp)
}

func (c *Client) executeFastHTTPWithStaleRetry(
	req *h1engine.Request,
	resp *h1engine.Response,
	do func() error,
	onRetry func(),
) error {
	err := do()
	if err != nil && isStaleKeepAliveError(err) {
		fastRespReset(resp)
		onRetry()
		req.SetConnectionClose()

		return c.engine.Do(req, resp)
	}

	return err
}

// isStaleKeepAliveError inspects socket read errors for broken pipes or EOFs caused by expired keep-alives.
func isStaleKeepAliveError(err error) bool {
	if err == nil {
		return false
	}

	errBytes := bytesconv.S2B(err.Error())

	return bytesconv.ContainsFoldASCII(errBytes, "connection closed") ||
		bytesconv.ContainsFoldASCII(errBytes, "closed connection") ||
		bytesconv.ContainsFoldASCII(errBytes, "broken pipe") ||
		bytesconv.ContainsFoldASCII(errBytes, "eof") ||
		bytesconv.ContainsFoldASCII(errBytes, "reading response headers") ||
		bytesconv.ContainsFoldASCII(errBytes, "use of closed") ||
		bytesconv.ContainsFoldASCII(errBytes, "reset by peer")
}

// fastRespReset resets fasthttp response buffers safely.
func fastRespReset(resp *h1engine.Response) {
	if resp != nil {
		resp.Reset()
	}
}

// ensureConnectionTE ensures 'Connection: TE' is present if a 'TE' header is configured on the request.
// RFC 9112 §7.4 / RFC 9110 §7.6.1: A sender of the TE header field MUST also include "TE" within the Connection
// header field to prevent hop-by-hop forwarding by intermediaries that do not support its semantics.
func ensureConnectionTE(req *h1engine.Request) {
	te := req.Header.Peek("TE")
	if len(te) == 0 {
		return
	}

	existingConn := req.Header.Peek("Connection")
	if bytesconv.ContainsFoldASCII(existingConn, "te") {
		return
	}

	if len(existingConn) > 0 {
		req.Header.Set("Connection", bytesconv.B2S(existingConn)+", TE")
	} else {
		req.Header.Set("Connection", "TE")
	}
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

func sanitizeTraceHeaders(req *h1engine.Request) {
	if bytesconv.EqualFoldASCII(bytesconv.B2S(req.Header.Method()), fheader.MethodTrace) {
		req.Header.Del(fheader.Authorization)
		req.Header.Del(fheader.ProxyAuthorization)
		req.Header.Del(fheader.Cookie)
	}
}
