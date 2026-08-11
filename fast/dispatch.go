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

	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
	"github.com/lemon4ksan/aoni/internal/pool"
)

// dispatchSingleRequest routes an HTTP request through Happy Eyeballs v3 (Protocol Racing),
// racing HTTP/3 (QUIC) against HTTP/2/HTTP/1 (TCP/TLS) with a staggered fallback timer.
func (c *Client) dispatchSingleRequest(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	sanitizeTraceHeaders(fastReq)

	host := string(fastReq.URI().Host())
	alpnMode := resolveALPNMode(ctx, &c.config, fastReq)

	staggerDelay := c.config.Network.HappyEyeballsDelay
	if alpnMode == aoni.AlpnH3 && c.shouldRaceProtocols(ctx) {
		return c.raceProtocolHandshakes(ctx, host, fastReq, fastResp, staggerDelay)
	}

	if alpnMode == aoni.AlpnH3 {
		if tr, h3Err, handled := c.tryDispatchH3(ctx, host, fastReq, fastResp); handled {
			return tr, h3Err, false
		}

		alpnMode = resolveALPNMode(ctx, &c.config, fastReq)
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
	resp         *fasthttp.Response
}

func (c *Client) raceProtocolHandshakes(
	ctx context.Context,
	host string,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
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
		h3Resp := fasthttp.AcquireResponse()

		tr, h3Err, handled := c.tryDispatchH3(raceCtx, host, fastReq, h3Resp)
		if handled && h3Err == nil {
			results <- raceResult{trailers: tr, err: nil, isH3: true, resp: h3Resp}
			return
		}

		fasthttp.ReleaseResponse(h3Resp)

		results <- raceResult{err: h3Err, isH3: true}
	}()

	staggerTimer := pool.AcquireTimer(staggerDelay)
	defer pool.ReleaseTimer(staggerTimer)

	var tcpStarted bool

	select {
	case res := <-results:
		if res.isH3 && res.err == nil {
			res.resp.CopyTo(fastResp)
			fasthttp.ReleaseResponse(res.resp)
			return res.trailers, nil, false
		}

	case <-staggerTimer.C:
		tcpStarted = true

		go func() {
			tcpResp := fasthttp.AcquireResponse()

			tr, tcpErr, released := c.dispatchH1OrH2(raceCtx, host, fastReq, tcpResp)
			if tcpErr == nil {
				results <- raceResult{trailers: tr, err: nil, autoReleased: released, isH3: false, resp: tcpResp}
				return
			}

			fasthttp.ReleaseResponse(tcpResp)

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
				fasthttp.ReleaseResponse(res.resp)
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
	for range 2 {
		select {
		case res := <-results:
			if res.resp != nil {
				fasthttp.ReleaseResponse(res.resp)
			}
		case <-time.After(2 * time.Second):
			return
		}
	}
}

func (c *Client) dispatchH1OrH2(
	ctx context.Context,
	host string,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
) (map[string][]string, error, bool) {
	alpnMode := resolveALPNMode(ctx, &c.config, fastReq)
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
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
) (map[string][]string, error, bool) {
	h3 := c.getH3Client()

	tr, err := h3.Do(ctx, fastReq, fastResp, c.config.Fingerprint.HeaderOrder)
	if err != nil {
		globalAltSvcCache.MarkH3Failed(host)
		fastResp.Reset()

		return nil, err, false
	}

	if c.isRecoverableStatus(fastResp.StatusCode()) {
		tr, errRec, _ := c.recoverSpecialStatus(ctx, host, fastReq, fastResp)
		return tr, errRec, true
	}

	globalAltSvcCache.MarkH3Success(host)

	return tr, nil, true
}

func (c *Client) tryDispatchH2(
	ctx context.Context,
	host string,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
) (map[string][]string, error, bool) {
	h2Cl := c.getH2Client(host)

	tr, err := h2Cl.DoWithTrailers(ctx, fastReq, fastResp)
	if err != nil && c.config.Fingerprint.BrowserID != aoni.BrowserNone {
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
		trRec, errRec, _ := c.recoverSpecialStatus(ctx, host, fastReq, fastResp)

		return trRec, errRec, true
	}

	recordAltSvcIfPresent(host, fastResp)

	return tr, nil, true
}

func (c *Client) dispatchH1WithFallbacks(
	ctx context.Context,
	host string,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
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
		tr, errRec, released := c.recoverSpecialStatus(ctx, host, fastReq, fastResp)
		return tr, errRec, released
	}

	recordAltSvcIfPresent(host, fastResp)

	return nil, nil, false
}

func (c *Client) fallbackH1ToH2(
	ctx context.Context,
	host string,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
) (map[string][]string, error, bool) {
	fastResp.Reset()

	h2Cl := c.getH2Client(host)

	tr, err := h2Cl.DoWithTrailers(ctx, fastReq, fastResp)
	if err != nil {
		return nil, err, false
	}

	if c.isRecoverableStatus(fastResp.StatusCode()) {
		c.removeH2Client(host)
		trRec, errRec, released := c.recoverSpecialStatus(ctx, host, fastReq, fastResp)

		return trRec, errRec, released
	}

	recordAltSvcIfPresent(host, fastResp)

	return tr, nil, false
}

func (c *Client) isRecoverableStatus(code int) bool {
	return code == http.StatusMisdirectedRequest ||
		code == http.StatusRequestTimeout ||
		code == http.StatusTooEarly
}

func (c *Client) recoverSpecialStatus(
	ctx context.Context,
	host string,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
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
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	reqCfg := aoni.GetOrInitRequestConfig(ctx)
	reqCfg.Disable0RTT = true

	host := string(fastReq.URI().Host())
	c.removeH2Client(host)

	fastReq.Header.Del("Early-Data")

	return c.dispatchSingleRequest(ctx, fastReq, fastResp)
}

func (c *Client) retry421Misdirected(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	reqCfg := aoni.GetOrInitRequestConfig(ctx)
	reqCfg.DisableAltSvc = true

	host := string(fastReq.URI().Host())
	globalAltSvcCache.MarkH3Failed(host)
	c.removeH2Client(host)

	fastReq.Header.Del("Alt-Svc")

	return c.dispatchSingleRequest(ctx, fastReq, fastResp)
}

func (c *Client) retry408Timeout(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	host := string(fastReq.URI().Host())
	c.removeH2Client(host)
	fastReq.SetConnectionClose()

	return c.dispatchSingleRequest(ctx, fastReq, fastResp)
}

func recordAltSvcIfPresent(host string, fastResp *fasthttp.Response) {
	if altSvc := fastResp.Header.Peek("Alt-Svc"); len(altSvc) > 0 {
		globalAltSvcCache.Record(host, string(altSvc))
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

	ensureConnectionTE(req)

	isHTTPS := bytes.EqualFold(req.URI().Scheme(), []byte("https"))
	origHost := req.URI().Host()
	hasHostHeader := len(req.Header.Peek("Host")) > 0

	if isHTTPS {
		hostStr := string(origHost)
		if !hasHostHeader {
			req.Header.SetHostBytes(origHost)
		}

		if !strings.Contains(hostStr, ":") {
			hostStr += ":443"
			req.URI().SetHost(hostStr)
		}

		c.TrackHTTPSTarget(hostStr)
		defer c.UntrackHTTPSTarget(hostStr)

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

func ensureConnectionTE(req *fasthttp.Request) {
	te := req.Header.Peek("TE")
	if len(te) == 0 {
		return
	}

	existingConn := string(req.Header.Peek("Connection"))
	if strings.Contains(strings.ToLower(existingConn), "te") {
		return
	}

	if existingConn != "" {
		req.Header.Set("Connection", existingConn+", TE")
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

func sanitizeTraceHeaders(req *fasthttp.Request) {
	if bytesconv.EqualFoldASCII(bytesconv.B2S(req.Header.Method()), "TRACE") {
		req.Header.Del("Authorization")
		req.Header.Del("Proxy-Authorization")
		req.Header.Del("Cookie")
	}
}
