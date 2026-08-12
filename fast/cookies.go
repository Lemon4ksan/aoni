// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// FILE: fast/cookies.go

package fast

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
	internalCookie "github.com/lemon4ksan/aoni/internal/cookie"
	"github.com/lemon4ksan/aoni/internal/urlutil"
	"github.com/lemon4ksan/aoni/netutil"
)

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

	u, err := urlutil.Parse(bytesconv.B2S(req.URI().FullURI()))
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

	u, err := urlutil.Parse(bytesconv.B2S(req.URI().FullURI()))
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
	return internalCookie.ParseSingleCookie(key, value)
}

func extractUserInfoAndSetAuth(req *fasthttp.Request) {
	uBytes := req.URI().Username()
	if len(uBytes) == 0 {
		return
	}

	user := bytesconv.B2S(uBytes)
	pass := bytesconv.B2S(req.URI().Password())

	if len(req.Header.Peek("Authorization")) == 0 {
		totalLen := len(user) + 1 + len(pass)
		if totalLen <= 128 {
			var buf [128]byte

			n := copy(buf[:], user)
			buf[n] = ':'
			copy(buf[n+1:], pass)

			encoded := base64.StdEncoding.EncodeToString(buf[:totalLen])
			req.Header.Set("Authorization", "Basic "+encoded)
		} else {
			encoded := base64.StdEncoding.EncodeToString(bytesconv.S2B(user + ":" + pass))
			req.Header.Set("Authorization", "Basic "+encoded)
		}
	}

	req.URI().SetUsername("")
	req.URI().SetPassword("")
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
	clean1 := netutil.CleanHost(h1)
	clean2 := netutil.CleanHost(h2)

	return urlutil.IsSameDomainOrSubdomain(clean1, clean2)
}
