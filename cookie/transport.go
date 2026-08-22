// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import (
	"net/http"
)

// Transport intercepts HTTP transactions to manage proxy-isolated cookies.
type Transport struct {
	Next      http.RoundTripper
	CookieJar *ProxyIsolatedJar
}

// RoundTrip injects isolated proxy cookies into outbound headers and captures response set-cookie headers.
// Safe for concurrent execution across multiple goroutines. Clones outbound request before header mutation to comply with [http.RoundTripper] contracts.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	next := t.Next
	if next == nil {
		next = http.DefaultTransport
	}

	reqToPass := t.setCookies(req)

	resp, err := next.RoundTrip(reqToPass)
	if err != nil || resp == nil || t.CookieJar == nil || reqToPass.URL == nil {
		return resp, err
	}

	setCookies := resp.Cookies()
	if len(setCookies) == 0 {
		return resp, nil
	}

	if jar := t.CookieJar.GetJar(reqToPass.Context()); jar != nil {
		jar.SetCookies(reqToPass.URL, setCookies)
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

// setCookies inspects target URL in req, retrieves matching cookies from the active jar,
// and returns a cloned request with populated 'Cookie' headers without mutating the input request.
func (t *Transport) setCookies(req *http.Request) *http.Request {
	if t.CookieJar == nil || req.URL == nil {
		return req
	}

	jar := t.CookieJar.GetJar(req.Context())
	if jar == nil {
		return req
	}

	cookies := jar.Cookies(req.URL)
	if len(cookies) == 0 {
		return req
	}

	cookieHeader := BuildCookieHeader(cookies)
	if cookieHeader == "" {
		return req
	}

	reqClone := req.Clone(req.Context())

	existing := reqClone.Header.Get("Cookie")
	if existing == "" {
		reqClone.Header.Set("Cookie", cookieHeader)
		return reqClone
	}

	reqClone.Header.Set("Cookie", existing+"; "+cookieHeader)

	return reqClone
}
