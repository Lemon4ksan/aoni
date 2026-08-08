// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"net/http"
	"net/url"
	"strings"
)

// AllowedDomainsRedirectPolicy constructs an [http.Client.CheckRedirect] policy function
// restricting HTTP redirects strictly to allowed domain patterns.
//
// Pattern Matching Rules:
//   - Exact domain matches: "example.com" matches "example.com" (and FQDN "example.com.").
//   - Wildcard subdomains: "*.example.com" matches "sub.example.com" and "example.com".
//   - Cross-origin header scrubbing is automatically enforced for untrusted targets.
func AllowedDomainsRedirectPolicy(allowedDomains ...string) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return &Error{Op: "redirect", Err: ErrMaxRedirectsExceeded}
		}

		if req.URL == nil {
			return nil
		}

		host := strings.ToLower(strings.TrimSuffix(req.URL.Hostname(), "."))
		for _, domainPattern := range allowedDomains {
			if matchDomainPattern(host, domainPattern) {
				return nil
			}
		}

		return &Error{Op: "redirect", Target: host, Err: ErrRedirectDomainForbidden}
	}
}

// DefaultRedirectPolicy constructs an [http.Client.CheckRedirect] policy function enforcing
// redirect chain length limits and scrubbing sensitive authentication headers during cross-origin
// or HTTPS-to-HTTP downgrade redirects (RFC 9110 §15.4 / RFC 7231 §6.4).
//
// Security & Header Scrubbing (RFC 9110 §15.4):
// When a redirect targets a different web origin (RFC 6454), sensitive credentials
// (Authorization, Cookie, X-Api-Key, Session tokens) are automatically purged from
// the redirected request to prevent token leakage to untrusted origins.
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

// applyRedirectPolicy applies redirect limit policies to the standard http.Client engine.
func applyRedirectPolicy(httpClient *http.Client, eng EngineConfig) {
	if eng.CheckRedirect != nil {
		httpClient.CheckRedirect = eng.CheckRedirect
		return
	}

	limit := eng.RedirectLimit
	if limit == RedirectLimitUnset {
		return
	}

	switch {
	case limit == 0:
		// RFC 9110: Return 3xx response directly without following redirects
		httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	case limit > 0:
		httpClient.CheckRedirect = DefaultRedirectPolicy(limit)
	default:
		httpClient.CheckRedirect = DefaultRedirectPolicy(10)
	}
}

// matchDomainPattern checks if host matches pattern (supporting exact and *.wildcard matches).
func matchDomainPattern(host, pattern string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	p := strings.ToLower(strings.TrimSuffix(pattern, "."))

	if !strings.HasPrefix(p, "*.") {
		return h == p
	}

	suffix := p[1:] // ".example.com"

	return strings.HasSuffix(h, suffix) || h == p[2:]
}

// isCrossOrigin determines whether u1 and u2 belong to different RFC 6454 web origins.
// Compares normalized Scheme, Hostname, and Canonical Port (accounting for default ports 80/443).
func isCrossOrigin(u1, u2 *url.URL) bool {
	if u1 == nil || u2 == nil {
		return false
	}

	// 1. Compare Scheme (https vs http)
	if !strings.EqualFold(u1.Scheme, u2.Scheme) {
		return true
	}

	// 2. Compare Hostname (ignoring FQDN trailing dots and ports)
	h1 := strings.ToLower(strings.TrimSuffix(u1.Hostname(), "."))

	h2 := strings.ToLower(strings.TrimSuffix(u2.Hostname(), "."))
	if h1 != h2 {
		return true
	}

	// 3. Compare Canonical Port
	return canonicalPort(u1) != canonicalPort(u2)
}

// canonicalPort resolves effective port number considering scheme defaults (80 for http, 443 for https).
func canonicalPort(u *url.URL) string {
	if u == nil {
		return ""
	}

	port := u.Port()
	if port != "" {
		return port
	}

	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}

	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}

	return ""
}
