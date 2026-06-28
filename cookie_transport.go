// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import "net/http"

// cookieJarTransport intercepts requests and responses at the transport level,
// providing context-safe cookie isolation based on the active proxy server.
type cookieJarTransport struct {
	next      http.RoundTripper
	cookieJar *ProxyIsolatedCookieJar
}

// RoundTrip automatically injects cookies before sending and extracts them from the response.
// Works correctly for every redirect, preserving the original request's context.
func (t *cookieJarTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	jar := t.cookieJar.GetJar(req.Context())
	if jar != nil {
		for _, cookie := range jar.Cookies(req.URL) {
			req.AddCookie(cookie)
		}
	}

	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if jar != nil {
		if rc := resp.Cookies(); len(rc) > 0 {
			jar.SetCookies(req.URL, rc)
		}
	}

	return resp, nil
}

// Unwrap returns the underlying transport, allowing http.Client.Clone
// to properly unwrap and re-wrap the transport chain.
func (t *cookieJarTransport) Unwrap() http.RoundTripper {
	return t.next
}
