// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"

	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
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

	u, err := url.Parse(string(req.URI().FullURI()))
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

	u, err := url.Parse(string(req.URI().FullURI()))
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
	header := http.Header{}
	header.Add("Set-Cookie", string(value))

	fakeResp := &http.Response{Header: header}

	parsed := fakeResp.Cookies()
	if len(parsed) > 0 {
		return parsed[0]
	}

	return nil
}

func extractUserInfoAndSetAuth(req *fasthttp.Request) {
	fullURI := req.URI().FullURI()
	if bytes.IndexByte(fullURI, '@') == -1 {
		return
	}

	userInfo := req.URI().Username()
	if len(userInfo) == 0 {
		return
	}

	if len(req.Header.Peek("Authorization")) == 0 {
		user := string(req.URI().Username())
		pass := string(req.URI().Password())
		encoded := base64.StdEncoding.EncodeToString(bytesconv.S2B(user + ":" + pass))
		req.Header.Set("Authorization", "Basic "+encoded)
	}

	req.URI().SetUsername("")
}
