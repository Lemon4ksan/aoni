// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// FILE: fast/cookies.go

package fast

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/url"

	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/foundation/bytesconv"
	internalCookie "github.com/lemon4ksan/aoni/internal/cookie"
	"github.com/lemon4ksan/aoni/internal/urlutil"
	"github.com/lemon4ksan/aoni/netutil"
)

// applyCookies populates outbound fasthttp request headers with matching cookies from the active jar.
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

	cookieHeader := internalCookie.BuildCookieHeader(cookies)
	if cookieHeader == "" {
		return
	}

	if existing := req.Header.Peek("Cookie"); len(existing) > 0 {
		req.Header.Set("Cookie", bytesconv.B2S(existing)+"; "+cookieHeader)
	} else {
		req.Header.Set("Cookie", cookieHeader)
	}
}

// captureCookies extracts response Set-Cookie headers and saves valid cookies to the active jar.
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

// parseCookie parses Set-Cookie header value bytes into an *http.Cookie without synthetic HTTP response allocations.
func parseCookie(_, value []byte) *http.Cookie {
	if len(value) == 0 {
		return nil
	}

	dto := internalCookie.ParseSetCookieHeader(bytesconv.B2S(value), "", "")
	if dto.Name == "" {
		return nil
	}

	var sameSite http.SameSite
	switch dto.SameSite {
	case "Lax":
		sameSite = http.SameSiteLaxMode
	case "Strict":
		sameSite = http.SameSiteStrictMode
	case "None":
		sameSite = http.SameSiteNoneMode
	}

	return &http.Cookie{ //nolint:gosec
		Name:        dto.Name,
		Value:       dto.Value,
		Domain:      dto.Domain,
		Path:        dto.Path,
		Expires:     dto.Expires,
		HttpOnly:    dto.HTTPOnly,
		Secure:      dto.Secure,
		SameSite:    sameSite,
		Partitioned: dto.Partitioned,
		MaxAge:      dto.MaxAge,
	}
}

// extractUserInfoAndSetAuth inspects URI credentials and constructs HTTP Basic Authorization headers if missing.
func extractUserInfoAndSetAuth(req *fasthttp.Request) {
	if len(req.Header.Peek("Authorization")) > 0 {
		return
	}

	uBytes := req.URI().Username()
	if len(uBytes) > 0 {
		user := bytesconv.B2S(uBytes)
		pass := bytesconv.B2S(req.URI().Password())
		encoded := base64.StdEncoding.EncodeToString(bytesconv.S2B(user + ":" + pass))
		req.Header.Set("Authorization", "Basic "+encoded)
	} else {
		rawURI := req.URI().FullURI()
		if bytes.IndexByte(rawURI, '@') >= 0 {
			if parsed, err := url.Parse(bytesconv.B2S(rawURI)); err == nil && parsed.User != nil {
				user := parsed.User.Username()
				pass, _ := parsed.User.Password()
				encoded := base64.StdEncoding.EncodeToString(bytesconv.S2B(user + ":" + pass))
				req.Header.Set("Authorization", "Basic "+encoded)
			}
		}
	}

	req.URI().SetUsername("")
	req.URI().SetPassword("")
}

// scrubSensitiveHeaders strips sensitive credentials and cookie headers upon cross-domain redirects per RFC 9110 §15.4.
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

// isSameDomainOrSubdomain reports whether h1 and h2 belong to the same domain or subdomain hierarchy.
func isSameDomainOrSubdomain(h1, h2 string) bool {
	clean1 := netutil.CleanHost(h1)
	clean2 := netutil.CleanHost(h2)

	return urlutil.IsSameDomainOrSubdomain(clean1, clean2)
}
