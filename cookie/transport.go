// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import (
	"net/http"
	"strings"
)

// Transport intercepts HTTP transactions to manage proxy-isolated cookies.
type Transport struct {
	Next      http.RoundTripper
	CookieJar *ProxyIsolatedJar
}

// RoundTrip injects isolated proxy cookies into outbound headers and captures response set-cookie headers.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	next := t.Next
	if next == nil {
		next = http.DefaultTransport
	}

	t.setCookies(req)

	resp, err := next.RoundTrip(req)
	if err != nil || resp == nil || t.CookieJar == nil || req.URL == nil {
		return resp, err
	}

	jar := t.CookieJar.GetJar(req.Context())
	if jar != nil {
		if rc := resp.Cookies(); len(rc) > 0 {
			jar.SetCookies(req.URL, rc)
		}
	}

	return resp, nil
}

// Unwrap returns the next wrapped [http.RoundTripper] layer.
func (t *Transport) Unwrap() http.RoundTripper {
	return t.Next
}

// CloneTransport creates a copy of [Transport] wrapping next.
func (t *Transport) CloneTransport(next http.RoundTripper) http.RoundTripper {
	return &Transport{
		Next:      next,
		CookieJar: t.CookieJar,
	}
}

func (t *Transport) setCookies(req *http.Request) {
	if t.CookieJar == nil || req.URL == nil {
		return
	}

	jar := t.CookieJar.GetJar(req.Context())
	if jar == nil {
		return
	}

	cookies := jar.Cookies(req.URL)
	if len(cookies) == 0 {
		return
	}

	cookieHeader := BuildCookieHeader(cookies)
	if cookieHeader == "" {
		return
	}

	existing := req.Header.Get("Cookie")
	if existing == "" {
		req.Header.Set("Cookie", cookieHeader)
		return
	}

	var sb strings.Builder
	sb.Grow(len(existing) + 2 + len(cookieHeader))
	sb.WriteString(existing)
	sb.WriteString("; ")
	sb.WriteString(cookieHeader)

	req.Header.Set("Cookie", sb.String())
}
