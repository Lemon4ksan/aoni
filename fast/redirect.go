// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"bytes"
	"context"
	"net/http"

	"github.com/lemon4ksan/foundation/net/http/zerocopy"
	machhttp "github.com/lemon4ksan/mach/proto/http"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/net/http/header"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
)

func (c *Client) executeWithRedirects(
	ctx context.Context,
	fastReq *machhttp.Request,
	fastResp *machhttp.Response,
) (trailers map[string][]string, err error, autoReleased bool) {
	redirectLimit := generic.Ternary(c.cfg.Engine.RedirectLimit < 0, 10, c.cfg.Engine.RedirectLimit)

	if redirectLimit == 0 {
		c.applyCookies(ctx, fastReq)
		extractUserInfoAndSetAuth(fastReq)

		trailers, err, autoReleased = c.execute(fastReq, fastResp)
		if err == nil {
			c.captureCookies(ctx, fastReq, fastResp)
		}

		return trailers, err, autoReleased
	}

	currentURI := zerocopy.AcquireURI()
	defer zerocopy.ReleaseURI(currentURI)

	var redirectsFollowed int

	for {
		c.applyCookies(ctx, fastReq)
		fastReq.URI().CopyTo(currentURI)
		extractUserInfoAndSetAuth(fastReq)

		trailers, err, autoReleased = c.execute(fastReq, fastResp)
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

		nextURI := zerocopy.AcquireURI()
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
			fastReq.Header.Del("Referer")
		} else {
			fastReq.Header.SetBytesKV(bytesconv.S2B("Referer"), currentURI.FullURI())
		}

		zerocopy.ReleaseURI(nextURI)
		fastResp.Reset()
	}
}

func isRedirectStatus(code int) bool {
	return code == http.StatusMovedPermanently ||
		code == http.StatusFound ||
		code == http.StatusSeeOther ||
		code == http.StatusTemporaryRedirect ||
		code == http.StatusPermanentRedirect
}

func isSameHost(u1, u2 *zerocopy.URI) bool {
	return bytes.EqualFold(u1.Host(), u2.Host())
}

func isHTTPSDowngrade(u1, u2 *zerocopy.URI) bool {
	return bytes.EqualFold(u1.Scheme(), []byte("https")) && bytes.EqualFold(u2.Scheme(), []byte("http"))
}

// applyRedirectMethodAndBody changes request method to GET and scrubs representation/content headers
// upon 301, 302, and 303 redirects per RFC 9110 §15.4 and §6.4.2.
func applyRedirectMethodAndBody(statusCode int, req *machhttp.Request) {
	switch statusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther:
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
