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

	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

func (c *Client) dispatchSingleRequest(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	sanitizeTraceHeaders(fastReq)

	host := string(fastReq.URI().Host())
	alpnMode := resolveALPNMode(ctx, &c.config, fastReq)

	// 1. HTTP/3 Execution
	if alpnMode == aoni.AlpnH3 {
		h3 := c.getH3Client()

		tr, err := h3.Do(ctx, fastReq, fastResp, c.config.Fingerprint.HeaderOrder)
		if err == nil {
			if fastResp.StatusCode() == http.StatusMisdirectedRequest {
				globalAltSvcCache.MarkH3Failed(host)
				fastResp.Reset()
				return c.retry421Misdirected(ctx, fastReq, fastResp)
			}

			return tr, nil, false
		}

		globalAltSvcCache.MarkH3Failed(host)
		fastResp.Reset()

		alpnMode = resolveALPNMode(ctx, &c.config, fastReq)
	}

	// 2. HTTP/2 Execution
	if alpnMode == aoni.AlpnH2 {
		h2Cl := c.getH2Client(host)

		tr, err := h2Cl.DoWithTrailers(ctx, fastReq, fastResp)
		if err == nil {
			if fastResp.StatusCode() == http.StatusMisdirectedRequest {
				c.removeH2Client(host)
				fastResp.Reset()
				return c.retry421Misdirected(ctx, fastReq, fastResp)
			}

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
				if fastResp.StatusCode() == http.StatusMisdirectedRequest {
					c.removeH2Client(host)
					fastResp.Reset()
					return c.retry421Misdirected(ctx, fastReq, fastResp)
				}

				if altSvc := fastResp.Header.Peek("Alt-Svc"); len(altSvc) > 0 {
					globalAltSvcCache.Record(host, string(altSvc))
				}

				return trFresh, nil, false
			}

			c.removeH2Client(host)
			fastResp.Reset()
		}
	}

	// 3. HTTP/1.1 Execution with H2-frame Fallback and Alt-Svc Discovery
	err, autoReleased = c.executeFastHTTP(ctx, fastReq, fastResp)
	if autoReleased {
		return nil, err, true
	}

	if err != nil && isH2FrameOnH1Error(err) {
		fastResp.Reset()

		h2Cl := c.getH2Client(host)

		tr, h2Err := h2Cl.DoWithTrailers(ctx, fastReq, fastResp)
		if h2Err == nil {
			if fastResp.StatusCode() == http.StatusMisdirectedRequest {
				c.removeH2Client(host)
				fastResp.Reset()
				return c.retry421Misdirected(ctx, fastReq, fastResp)
			}

			if altSvc := fastResp.Header.Peek("Alt-Svc"); len(altSvc) > 0 {
				globalAltSvcCache.Record(host, string(altSvc))
			}

			return tr, nil, false
		}

		err = h2Err
	}

	if err == nil {
		if fastResp.StatusCode() == http.StatusMisdirectedRequest {
			fastResp.Reset()
			return c.retry421Misdirected(ctx, fastReq, fastResp)
		}

		if altSvc := fastResp.Header.Peek("Alt-Svc"); len(altSvc) > 0 {
			globalAltSvcCache.Record(host, string(altSvc))
		}
	}

	return nil, err, false
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

		if c.dialer != nil {
			c.dialer.TrackHTTPSTarget(hostStr)
			defer c.dialer.UntrackHTTPSTarget(hostStr)
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
