// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/lemon4ksan/aoni/cookie"
)

// FuzzParseSetCookieHeader fuzzes RFC 6265/6265bis Set-Cookie header parsing with arbitrary attribute values.
func FuzzParseSetCookieHeader(f *testing.F) {
	f.Add("session=abc1234; Domain=example.com; Path=/; Secure; HttpOnly; SameSite=Strict", "example.com", "/")
	f.Add("token=xyz; Max-Age=3600; Partitioned", "sub.example.com", "/api")
	f.Add("", "", "")
	f.Add("invalid_cookie_format_no_equals", "example.com", "/")
	f.Add("key=val; Expires=Wed, 21 Oct 2025 07:28:00 GMT", "example.com", "/")

	f.Fuzz(func(t *testing.T, headerVal, defaultDomain, defaultPath string) {
		c := cookie.ParseSetCookieHeader(headerVal, defaultDomain, defaultPath)
		if headerVal != "" {
			_ = c.Name
			_ = c.Value
			_ = c.Domain
			_ = c.Path
		}
	})
}

// FuzzNetscapeCookieExport tests Netscape/Curl cookie file export and sorting.
func FuzzNetscapeCookieExport(f *testing.F) {
	f.Add("https://example.com/api", "sid", "val123", "/", ".example.com")
	f.Add("http://localhost:8080/", "tok", "secret", "/sub", "")
	f.Add("", "", "", "", "")

	f.Fuzz(func(t *testing.T, rawURL, name, val, path, domain string) {
		u, err := url.Parse(rawURL)
		if err != nil || u.Host == "" {
			return
		}

		jar := cookie.NewProxyIsolatedJar()
		cookies := []*http.Cookie{
			{
				Name:   name,
				Value:  val,
				Path:   path,
				Domain: domain,
			},
		}

		jar.SetCookies(u, cookies)
		_ = cookie.ExportNetscape(jar, u)
		_ = cookie.Export(jar, u)

		cookie.SortForBrowser(cookies)
		_ = cookie.BuildCookieHeader(cookies)
	})
}

// FuzzProxyIsolatedJar tests proxy-isolated cookie jar storage against arbitrary URL inputs.
func FuzzProxyIsolatedJar(f *testing.F) {
	f.Add("https://example.com/api/v1", "socks5://127.0.0.1:1080", "session_id", "secret_val")
	f.Add("http://localhost:8080/", "", "user", "admin")
	f.Add("", "", "", "")

	f.Fuzz(func(t *testing.T, rawURL, proxyURL, cookieName, cookieVal string) {
		u, err := url.Parse(rawURL)
		if err != nil || u.Host == "" {
			return
		}

		jar := cookie.NewProxyIsolatedJar()
		cookies := []*http.Cookie{
			{
				Name:   cookieName,
				Value:  cookieVal,
				Domain: u.Hostname(),
				Path:   "/",
			},
		}

		jar.SetCookies(u, cookies)
		_, _ = jar.FindCookie(u, cookieName)
		_, _ = jar.GetCookieValue(u, cookieName)
		_ = jar.Cookies(u)
	})
}
