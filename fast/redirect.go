// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"bytes"
	"context"
	"net/http"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/valyala/fasthttp"
)

func (c *Client) executeWithRedirects(
	ctx context.Context,
	fastReq *fasthttp.Request,
	fastResp *fasthttp.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	redirectLimit := c.cfg.Engine.RedirectLimit
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

		if c.referer != nil {
			c.referer.LastURL.Set(string(currentURI.FullURI()))
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

// applyRedirectMethodAndBody changes request method to GET and scrubs representation/content headers
// upon 301, 302, and 303 redirects per RFC 9110 §15.4 and §6.4.2.
func applyRedirectMethodAndBody(statusCode int, req *fasthttp.Request) {
	switch statusCode {
	case fasthttp.StatusMovedPermanently, fasthttp.StatusFound, fasthttp.StatusSeeOther:
		method := string(req.Header.Method())
		if method != http.MethodGet && method != http.MethodHead {
			req.Header.SetMethod(http.MethodGet)
			req.SetBody(nil)
			req.Header.Del("Content-Type")
			req.Header.Del("Content-Length")
			req.Header.Del("Content-Encoding")
			req.Header.Del("Content-Language")
			req.Header.Del("Content-Location")
			req.Header.Del("Digest")
		}
	}
}
