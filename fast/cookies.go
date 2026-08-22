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

	impl "github.com/lemon4ksan/foundation/net/cookie"
	furl "github.com/lemon4ksan/foundation/net/url"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/netutil"
)

// applyCookies populates outbound fasthttp request headers with matching cookies from the active jar.
func (c *Client) applyCookies(ctx context.Context, req *fasthttp.Request) {
	jar := c.cfg.Engine.CookieJar
	if jar == nil {
		return
	}

	if pJar, ok := jar.(*cookie.ProxyIsolatedJar); ok {
		jar = pJar.GetJar(ctx)
	}

	u := uriToURL(req.URI())

	cookies := jar.Cookies(u)
	if len(cookies) == 0 {
		return
	}

	cookieHeader := impl.BuildCookieHeader(cookies)
	if cookieHeader == "" {
		return
	}

	if existing := req.Header.Peek("Cookie"); len(existing) > 0 {
		var stackBuf [512]byte

		needed := len(existing) + 2 + len(cookieHeader)

		var buf []byte
		if needed <= len(stackBuf) {
			buf = stackBuf[:0]
		} else {
			buf = make([]byte, 0, needed)
		}

		buf = append(buf, existing...)
		buf = append(buf, ';', ' ')
		buf = append(buf, cookieHeader...)
		req.Header.SetBytesKV(bytesconv.S2B("Cookie"), buf)
	} else {
		req.Header.Set("Cookie", cookieHeader)
	}
}

// captureCookies extracts response Set-Cookie headers and saves valid cookies to the active jar.
func (c *Client) captureCookies(ctx context.Context, req *fasthttp.Request, resp *fasthttp.Response) {
	jar := c.cfg.Engine.CookieJar
	if jar == nil {
		return
	}

	if pJar, ok := jar.(*cookie.ProxyIsolatedJar); ok {
		jar = pJar.GetJar(ctx)
	}

	if jar == nil {
		return
	}

	u := uriToURL(req.URI())

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

	dto := impl.ParseSetCookieHeader(bytesconv.B2S(value), "", "")
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

		return
	}

	host := req.URI().Host()
	if bytes.IndexByte(host, '@') < 0 {
		return
	}

	rawURI := req.URI().FullURI()
	if parsed, err := url.Parse(bytesconv.B2S(rawURI)); err == nil && parsed.User != nil {
		user := parsed.User.Username()
		pass, _ := parsed.User.Password()
		encoded := base64.StdEncoding.EncodeToString(bytesconv.S2B(user + ":" + pass))
		req.Header.Set("Authorization", "Basic "+encoded)
	}

	req.URI().SetUsername("")
	req.URI().SetPassword("")
}

// scrubSensitiveHeaders strips sensitive credentials and cookie headers upon cross-domain redirects per RFC 9110 §15.4.
func scrubSensitiveHeaders(req *fasthttp.Request, currentURI, nextURI *fasthttp.URI) {
	req.Header.Del("Authorization")
	req.Header.Del("Proxy-Authorization")
	req.Header.Del("Proxy-Authenticate")
	req.Header.Del("WWW-Authenticate")
	req.Header.Del("Cookie2")
	req.Header.Del("X-Api-Key")
	req.Header.Del("X-Auth-Token")
	req.Header.Del("X-Access-Token")
	req.Header.Del("X-Secret")
	req.Header.Del("X-Client-Secret")
	req.Header.Del("Api-Key")
	req.Header.Del("Token")
	req.Header.Del("Secret")
	req.Header.Del("Private-Key")

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

	return furl.IsSameDomainOrSubdomain(clean1, clean2)
}

func uriToURL(uri *fasthttp.URI) *url.URL {
	return &url.URL{
		Scheme:   bytesconv.B2S(uri.Scheme()),
		Host:     bytesconv.B2S(uri.Host()),
		Path:     bytesconv.B2S(uri.Path()),
		RawQuery: bytesconv.B2S(uri.QueryArgs().QueryString()),
	}
}
