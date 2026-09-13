// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"bytes"
	"context"
	"net/http"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/mach/client/h1"
)

func (c *Client) executeWithRedirects(
	ctx context.Context,
	fastReq *h1.Request,
	fastResp *h1.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	redirectLimit := generic.Ternary(c.cfg.Engine.RedirectLimit < 0, 10, c.cfg.Engine.RedirectLimit)

	if redirectLimit == 0 {
		c.applyCookies(ctx, fastReq)
		extractUserInfoAndSetAuth(fastReq)

		trailers, err, autoReleased = c.dispatchSingleRequest(ctx, fastReq, fastResp)
		if err == nil {
			c.captureCookies(ctx, fastReq, fastResp)
		}

		return trailers, err, autoReleased
	}

	currentURI := h1.AcquireURI()
	defer h1.ReleaseURI(currentURI)

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

		location := fastResp.Header.Peek(header.Location)
		if len(location) == 0 {
			return trailers, nil, false
		}

		redirectsFollowed++
		if redirectsFollowed > redirectLimit {
			return nil, ErrMaxRedirectsExceeded, false
		}

		applyRedirectMethodAndBody(statusCode, fastReq)

		nextURI := h1.AcquireURI()
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

		if host := nextURI.Host(); len(host) > 0 {
			fastReq.Header.SetHostBytes(host)
		}

		if !isSameHost(currentURI, nextURI) {
			scrubSensitiveHeaders(fastReq, currentURI, nextURI)
		}

		if isHTTPSDowngrade(currentURI, nextURI) {
			fastReq.Header.Del(header.Referer)
		} else {
			fastReq.Header.SetBytesKV(bytesconv.S2B(header.Referer), currentURI.FullURI())
		}

		if c.referer != nil {
			c.referer.LastURL.Set(string(currentURI.FullURI()))
		}

		h1.ReleaseURI(nextURI)
		fastResp.Reset()
	}
}

func isRedirectStatus(code int) bool {
	return code == h1.StatusMovedPermanently ||
		code == h1.StatusFound ||
		code == h1.StatusSeeOther ||
		code == h1.StatusTemporaryRedirect ||
		code == h1.StatusPermanentRedirect
}

func isSameHost(u1, u2 *h1.URI) bool {
	return bytes.EqualFold(u1.Host(), u2.Host())
}

func isHTTPSDowngrade(u1, u2 *h1.URI) bool {
	return bytes.EqualFold(u1.Scheme(), []byte("https")) && bytes.EqualFold(u2.Scheme(), []byte("http"))
}

// applyRedirectMethodAndBody changes request method to GET and scrubs representation/content headers
// upon 301, 302, and 303 redirects per RFC 9110 §15.4 and §6.4.2.
func applyRedirectMethodAndBody(statusCode int, req *h1.Request) {
	switch statusCode {
	case h1.StatusMovedPermanently, h1.StatusFound, h1.StatusSeeOther:
		method := bytesconv.B2S(req.Header.Method())
		if method != http.MethodGet && method != http.MethodHead {
			req.Header.SetMethod(http.MethodGet)
			req.SetBody(nil)
			req.Header.Del(header.ContentType)
			req.Header.Del(header.ContentLength)
			req.Header.Del(header.ContentEncoding)
			req.Header.Del(header.ContentLanguage)
			req.Header.Del(header.ContentLocation)
			req.Header.Del(header.Digest)
		}
	}
}
