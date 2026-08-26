// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"bytes"
	"context"
	"net/http"

	fheader "github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
)

func (c *Client) executeWithRedirects(
	ctx context.Context,
	fastReq *h1engine.Request,
	fastResp *h1engine.Response,
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

	currentURI := h1engine.AcquireURI()
	defer h1engine.ReleaseURI(currentURI)

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

		location := fastResp.Header.Peek(fheader.Location)
		if len(location) == 0 {
			return trailers, nil, false
		}

		redirectsFollowed++
		if redirectsFollowed > redirectLimit {
			return nil, ErrMaxRedirectsExceeded, false
		}

		applyRedirectMethodAndBody(statusCode, fastReq)

		nextURI := h1engine.AcquireURI()
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
			fastReq.Header.Del(fheader.Referer)
		} else {
			fastReq.Header.SetBytesKV(bytesconv.S2B(fheader.Referer), currentURI.FullURI())
		}

		if c.referer != nil {
			c.referer.LastURL.Set(string(currentURI.FullURI()))
		}

		h1engine.ReleaseURI(nextURI)
		fastResp.Reset()
	}
}

func isRedirectStatus(code int) bool {
	return code == h1engine.StatusMovedPermanently ||
		code == h1engine.StatusFound ||
		code == h1engine.StatusSeeOther ||
		code == h1engine.StatusTemporaryRedirect ||
		code == h1engine.StatusPermanentRedirect
}

func isSameHost(u1, u2 *h1engine.URI) bool {
	return bytes.EqualFold(u1.Host(), u2.Host())
}

func isHTTPSDowngrade(u1, u2 *h1engine.URI) bool {
	return bytes.EqualFold(u1.Scheme(), []byte("https")) && bytes.EqualFold(u2.Scheme(), []byte("http"))
}

// applyRedirectMethodAndBody changes request method to GET and scrubs representation/content headers
// upon 301, 302, and 303 redirects per RFC 9110 §15.4 and §6.4.2.
func applyRedirectMethodAndBody(statusCode int, req *h1engine.Request) {
	switch statusCode {
	case h1engine.StatusMovedPermanently, h1engine.StatusFound, h1engine.StatusSeeOther:
		method := bytesconv.B2S(req.Header.Method())
		if method != http.MethodGet && method != http.MethodHead {
			req.Header.SetMethod(http.MethodGet)
			req.SetBody(nil)
			req.Header.Del(fheader.ContentType)
			req.Header.Del(fheader.ContentLength)
			req.Header.Del(fheader.ContentEncoding)
			req.Header.Del(fheader.ContentLanguage)
			req.Header.Del(fheader.ContentLocation)
			req.Header.Del(fheader.Digest)
		}
	}
}
