// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/lemon4ksan/foundation/generic"
	fcookie "github.com/lemon4ksan/foundation/net/cookie"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
)

// MaxCookieAgeSeconds defines the maximum recommended cookie lifetime in seconds (400 days / 34,560,000s)
// as mandated by RFC 6265bis §5.5.
const (
	MaxCookieAgeSeconds = fcookie.MaxCookieAgeSeconds
	MaxCookieAgeLimit   = fcookie.MaxCookieAgeLimit
)

// Cookie represents a browser cookie structure formatted for JSON persistence,
// including CHIPS (RFC 6265bis) Partitioned attributes and SameSite policies.
//
// Specification Adherence:
// Conforms to IETF RFC 6265 (HTTP State Management Mechanism) and RFC 6265bis
// (Cookies: HTTP State Management Mechanism - CHIPS Partitioned Cookies).
//
// Thread Safety:
// Struct values are pass-by-value DTOs; concurrent reads are safe after construction.
type Cookie = fcookie.Cookie

// ParseSetCookieHeader parses a raw 'Set-Cookie' header line into a structured [Cookie] (RFC 6265 §5.2, RFC 6265bis §5.5 & §5.7).
func ParseSetCookieHeader(headerVal, defaultDomain, defaultPath string) Cookie {
	return fcookie.ParseSetCookieHeader(headerVal, defaultDomain, defaultPath)
}

// ValidateCookiePrefix reports whether a cookie satisfies RFC 6265bis §4.1.3 & §5.4 cookie prefix rules:
//   - "__Secure-": MUST have Secure=true.
//   - "__Host-": MUST have Secure=true, Path="/", and empty Domain (host-only).
//   - Nameless cookies whose value begins with "__Secure-" or "__Host-" MUST be rejected (RFC 6265bis §5.7 step 22).
func ValidateCookiePrefix(c Cookie) bool {
	return fcookie.ValidatePrefix(c)
}

// FromStd converts a standard [*http.Cookie] into a structured [Cookie] (RFC 6265 §5.3).
func FromStd(c *http.Cookie, defaultDomain, defaultPath string) Cookie {
	if c == nil {
		return Cookie{}
	}

	domain := generic.Coalesce(c.Domain, defaultDomain)
	path := generic.Coalesce(c.Path, defaultPath)

	return Cookie{
		Name:     c.Name,
		Value:    c.Value,
		Domain:   strings.ToLower(domain),
		Path:     path,
		Expires:  c.Expires,
		HTTPOnly: c.HttpOnly,
		Secure:   c.Secure,
		MaxAge:   c.MaxAge,
	}
}

// PathMatch reports whether reqPath matches cookiePath according to RFC 6265 §5.1.4.
func PathMatch(reqPath, cookiePath string) bool {
	return fcookie.PathMatch(reqPath, cookiePath)
}

// FilterForRequest filters a slice of cookies, returning only those matching destination u per RFC 6265 §5.1.4.
func FilterForRequest(cookies []*http.Cookie, u *url.URL) []*http.Cookie {
	if len(cookies) == 0 || u == nil {
		return nil
	}

	reqPath := generic.Coalesce(u.Path, "/")

	return generic.Filter(cookies, func(c *http.Cookie) bool {
		return PathMatch(reqPath, c.Path)
	})
}

// Mirror copies specified cookies by name from sourceURL to each destination URL in targetURLs inside jar.
func Mirror(jar http.CookieJar, sourceURL *url.URL, targetURLs []*url.URL, cookieNames ...string) {
	if jar == nil || sourceURL == nil || len(targetURLs) == 0 || len(cookieNames) == 0 {
		return
	}

	cookies := jar.Cookies(sourceURL)
	if len(cookies) == 0 {
		return
	}

	var toMirror []*http.Cookie
	if len(cookieNames) > 8 {
		nameSet := generic.NewSet(cookieNames...)
		toMirror = generic.Filter(cookies, func(c *http.Cookie) bool {
			return nameSet.Has(c.Name)
		})
	} else {
		toMirror = generic.Filter(cookies, func(c *http.Cookie) bool {
			return slices.Contains(cookieNames, c.Name)
		})
	}

	if len(toMirror) == 0 {
		return
	}

	for _, target := range targetURLs {
		if target != nil {
			jar.SetCookies(target, toMirror)
		}
	}
}

// SortForBrowser sorts cookies in-place according to RFC 6265 §5.4 (longest path length first).
func SortForBrowser(cookies []*http.Cookie) {
	fcookie.SortForBrowser(cookies)
}

// BuildCookieHeader constructs an RFC 6265 compliant 'Cookie' request header string (RFC 6265 §4.2.1 & §5.4).
func BuildCookieHeader(cookies []*http.Cookie) string {
	return fcookie.BuildCookieHeader(cookies)
}

// ExportNetscape exports cookies formatted as a standard Netscape HTTP Cookie File (cookies.txt).
func ExportNetscape(jar http.CookieJar, u *url.URL) string {
	if jar == nil || u == nil {
		return ""
	}

	return fcookie.ExportNetscape(jar.Cookies(u), u.Hostname())
}

// Export converts cookies for u from jar into exported [Cookie] structures.
func Export(jar http.CookieJar, u *url.URL) []Cookie {
	if jar == nil || u == nil {
		return nil
	}

	rawCookies := jar.Cookies(u)
	if len(rawCookies) == 0 {
		return nil
	}

	return generic.Map(rawCookies, func(c *http.Cookie) Cookie {
		var sameSiteStr string
		switch c.SameSite {
		case http.SameSiteLaxMode:
			sameSiteStr = "Lax"
		case http.SameSiteStrictMode:
			sameSiteStr = "Strict"
		case http.SameSiteNoneMode:
			sameSiteStr = "None"
		}

		return Cookie{
			Name:        c.Name,
			Value:       c.Value,
			Domain:      strings.ToLower(c.Domain),
			Path:        c.Path,
			Expires:     c.Expires,
			HTTPOnly:    c.HttpOnly,
			Secure:      c.Secure,
			SameSite:    sameSiteStr,
			MaxAge:      c.MaxAge,
			Partitioned: c.Partitioned,
		}
	})
}

// ExportJSON serializes exported cookies for u into a JSON string.
func ExportJSON(jar http.CookieJar, u *url.URL) (string, error) {
	exported := Export(jar, u)
	if len(exported) == 0 {
		return "[]", nil
	}

	b, err := json.Marshal(exported)
	if err != nil {
		return "", err
	}

	return bytesconv.B2S(b), nil
}

// Import injects a slice of exported [Cookie] structs into jar for destination u.
func Import(jar http.CookieJar, u *url.URL, cookies []Cookie) {
	if jar == nil || u == nil || len(cookies) == 0 {
		return
	}

	httpCookies := generic.Map(cookies, func(c Cookie) *http.Cookie {
		var sameSite http.SameSite
		switch c.SameSite {
		case "Lax":
			sameSite = http.SameSiteLaxMode
		case "Strict":
			sameSite = http.SameSiteStrictMode
		case "None":
			sameSite = http.SameSiteNoneMode
		}

		return &http.Cookie{
			Name:        c.Name,
			Value:       c.Value,
			Domain:      c.Domain,
			Path:        c.Path,
			Expires:     c.Expires,
			HttpOnly:    c.HTTPOnly,
			Secure:      c.Secure,
			SameSite:    sameSite,
			MaxAge:      c.MaxAge,
			Partitioned: c.Partitioned,
		}
	})

	jar.SetCookies(u, httpCookies)
}

// ImportJSON deserializes a JSON cookie payload and imports it into jar for target u.
func ImportJSON(jar http.CookieJar, u *url.URL, jsonStr string) error {
	if jar == nil || u == nil || jsonStr == "" || jsonStr == "[]" {
		return nil
	}

	var cookies []Cookie
	if err := json.Unmarshal(bytesconv.S2B(jsonStr), &cookies); err != nil {
		return err
	}

	Import(jar, u, cookies)

	return nil
}
