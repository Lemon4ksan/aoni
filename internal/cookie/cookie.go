// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cookie implements a cookie parser and serializer for HTTP cookies.
// Core implementation is located in [github.com/lemon4ksan/foundation/net/cookie].
package cookie

import (
	"net/http"

	fcookie "github.com/lemon4ksan/foundation/net/cookie"
)

const (
	MaxCookieAgeSeconds = fcookie.MaxCookieAgeSeconds
	MaxCookieAgeLimit   = fcookie.MaxCookieAgeLimit
)

// Cookie represents an HTTP cookie structure.
type Cookie = fcookie.Cookie

// ParseSetCookieHeader parses a raw Set-Cookie header line with zero heap allocations.
func ParseSetCookieHeader(headerVal, defaultDomain, defaultPath string) Cookie {
	return fcookie.ParseSetCookieHeader(headerVal, defaultDomain, defaultPath)
}

// ParseCookieAttribute sets the corresponding field on [Cookie].
func ParseCookieAttribute(key, val string, c *Cookie) {
	fcookie.ParseCookieAttribute(key, val, c)
}

// ValidatePrefix verifies whether cookie conforms to prefix rules.
func ValidatePrefix(c Cookie) bool {
	return fcookie.ValidatePrefix(c)
}

// PathMatch reports whether reqPath matches cookiePath per RFC 6265 §5.1.4.
func PathMatch(reqPath, cookiePath string) bool {
	return fcookie.PathMatch(reqPath, cookiePath)
}

// SortForBrowser sorts cookies in-place per RFC 6265 §5.4.
func SortForBrowser(cookies []*http.Cookie) {
	fcookie.SortForBrowser(cookies)
}

// BuildCookieHeader constructs an RFC 6265 compliant Cookie request header string.
func BuildCookieHeader(cookies []*http.Cookie) string {
	return fcookie.BuildCookieHeader(cookies)
}

// ExportNetscape exports cookies as Netscape format string.
func ExportNetscape(cookies []*http.Cookie, defaultHost string) string {
	return fcookie.ExportNetscape(cookies, defaultHost)
}

// ParseSingleCookie parses key and value bytes into an http.Cookie pointer.
func ParseSingleCookie(key, value []byte) *http.Cookie {
	return fcookie.ParseSingleCookie(key, value)
}
