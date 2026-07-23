// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import "net/http"

// Transport intercepts HTTP round-trips to automatically inject and extract cookies
// in a [ProxyIsolatedJar] based on the request's active proxy context.
type Transport struct {
	Next      http.RoundTripper
	CookieJar *ProxyIsolatedJar
}

// RoundTrip injects isolated proxy cookies with browser-compliant RFC 6265 ordering
// into outgoing request headers and stores response cookies back into the proxy jar.
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

	if existing := req.Header.Get("Cookie"); existing != "" {
		req.Header.Set("Cookie", existing+"; "+cookieHeader)
	} else {
		req.Header.Set("Cookie", cookieHeader)
	}
}

// Unwrap returns the underlying [http.RoundTripper] for transport wrapper chaining.
func (t *Transport) Unwrap() http.RoundTripper {
	if t.Next != nil {
		return t.Next
	}

	return http.DefaultTransport
}
