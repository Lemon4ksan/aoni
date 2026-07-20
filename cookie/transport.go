// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import "net/http"

// Transport intercepts requests and responses at the transport level,
// providing context-safe cookie isolation based on the active proxy server.
type Transport struct {
	Next      http.RoundTripper
	CookieJar *ProxyIsolatedJar
}

// RoundTrip automatically injects cookies before sending and extracts them from the response.
// Works correctly for every redirect, preserving the original request's context.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	jar := t.CookieJar.GetJar(req.Context())
	if jar != nil {
		for _, cookie := range jar.Cookies(req.URL) {
			req.AddCookie(cookie)
		}
	}

	resp, err := t.Next.RoundTrip(req)
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
func (t *Transport) Unwrap() http.RoundTripper {
	return t.Next
}
