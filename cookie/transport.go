// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import "net/http"

// Transport intercepts HTTP round-trips to automatically inject and store cookies
// in a [ProxyIsolatedJar] based on the request's active proxy context.
type Transport struct {
	Next      http.RoundTripper
	CookieJar *ProxyIsolatedJar
}

// RoundTrip injects isolated proxy cookies into outgoing request headers
// and extracts response cookies back into the proxy jar.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	next := t.Next
	if next == nil {
		next = http.DefaultTransport
	}

	if t.CookieJar != nil {
		if jar := t.CookieJar.GetJar(req.Context()); jar != nil {
			for _, c := range jar.Cookies(req.URL) {
				req.AddCookie(c)
			}
		}
	}

	resp, err := next.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if t.CookieJar != nil && resp != nil {
		if jar := t.CookieJar.GetJar(req.Context()); jar != nil {
			if rc := resp.Cookies(); len(rc) > 0 {
				jar.SetCookies(req.URL, rc)
			}
		}
	}

	return resp, nil
}

// Unwrap returns the underlying [http.RoundTripper] for transport wrapper chaining.
func (t *Transport) Unwrap() http.RoundTripper {
	if t.Next != nil {
		return t.Next
	}

	return http.DefaultTransport
}
