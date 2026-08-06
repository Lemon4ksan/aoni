// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"net/http"
	"net/url"
	"strings"
)

// AllowedDomainsRedirectPolicy creates an [http.Client.CheckRedirect]
// restricting HTTP redirects strictly to allowed domain patterns.
//
// Supports exact domain matches ("example.com") and wildcard subdomains ("*.example.com").
func AllowedDomainsRedirectPolicy(allowedDomains ...string) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return &Error{Op: "redirect", Err: ErrMaxRedirectsExceeded}
		}

		if req.URL == nil {
			return nil
		}

		host := strings.ToLower(req.URL.Hostname())
		for _, domainPattern := range allowedDomains {
			if matchDomainPattern(host, domainPattern) {
				return nil
			}
		}

		return &Error{Op: "redirect", Target: host, Err: ErrRedirectDomainForbidden}
	}
}

// DefaultRedirectPolicy creates an [http.Client.CheckRedirect] function enforcing redirect limits
// and scrubbing sensitive authentication headers during cross-origin redirects.
func DefaultRedirectPolicy(
	maxRedirects int,
	sensitiveHeaders ...string,
) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if maxRedirects >= 0 && len(via) >= maxRedirects {
			return &Error{Op: "redirect", Err: ErrMaxRedirectsExceeded}
		}

		if len(via) == 0 {
			return nil
		}

		headersToScrub := sensitiveHeaders
		if len(headersToScrub) == 0 {
			headersToScrub = DefaultSensitiveHeaders
		}

		if isCrossOrigin(req.URL, via[0].URL) {
			for _, h := range headersToScrub {
				req.Header.Del(h)
			}
		}

		return nil
	}
}

func applyRedirectPolicy(httpClient *http.Client, eng EngineConfig) {
	if eng.CheckRedirect != nil {
		httpClient.CheckRedirect = eng.CheckRedirect
		return
	}

	limit := eng.RedirectLimit
	if limit == redirectLimitUnset {
		return
	}

	switch {
	case limit == 0:
		httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	case limit > 0:
		httpClient.CheckRedirect = DefaultRedirectPolicy(limit)
	default:
		httpClient.CheckRedirect = DefaultRedirectPolicy(10)
	}
}

func matchDomainPattern(host, pattern string) bool {
	p := strings.ToLower(pattern)
	if !strings.HasPrefix(p, "*.") {
		return host == p
	}

	suffix := p[1:]

	return strings.HasSuffix(host, suffix) || host == p[2:]
}

func isCrossOrigin(u1, u2 *url.URL) bool {
	return u1.Scheme != u2.Scheme || u1.Host != u2.Host
}
