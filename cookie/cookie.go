// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/lemon4ksan/foundation/codec/json"
	"github.com/lemon4ksan/foundation/generic"
	fcookie "github.com/lemon4ksan/foundation/net/cookie"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni/internal/core"
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

// ValidatePrefix reports whether a cookie satisfies RFC 6265bis §4.1.3 & §5.4 cookie prefix rules:
//   - "__Secure-": MUST have Secure=true.
//   - "__Host-": MUST have Secure=true, Path="/", and empty Domain (host-only).
//   - Nameless cookies whose value begins with "__Secure-" or "__Host-" MUST be rejected (RFC 6265bis §5.7 step 22).
func ValidatePrefix(c Cookie) bool {
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

// DomainMatch reports whether host matches cookieDomain per RFC 6265 §5.1.3.
func DomainMatch(host, cookieDomain string) bool {
	host = strings.ToLower(host)
	cookieDomain = strings.ToLower(cookieDomain)

	if host == cookieDomain {
		return true
	}

	if net.ParseIP(host) != nil {
		return false
	}

	if strings.HasSuffix(host, cookieDomain) {
		if len(host) > len(cookieDomain) && host[len(host)-len(cookieDomain)-1] == '.' {
			return true
		}
	}

	return false
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
		// RFC 6265 Section 5.4 Step 1: Missing domain-match enforcement (RFC 6265 Section 5.1.3).
		return PathMatch(reqPath, c.Path)
	})
}

// Mirror copies specified cookies by name from sourceURL to each destination URL in targetURLs inside jar.
func Mirror(ctx context.Context, jar core.CookieJar, sourceURL *url.URL, targetURLs []*url.URL, cookieNames ...string) {
	if jar == nil || sourceURL == nil || len(targetURLs) == 0 || len(cookieNames) == 0 {
		return
	}

	cookies := jar.Cookies(ctx, sourceURL)
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
			jar.SetCookies(ctx, target, toMirror)
		}
	}
}

// Export converts cookies for u from jar into exported [Cookie] structures.
func Export(ctx context.Context, jar core.CookieJar, u *url.URL) []Cookie {
	if jar == nil || u == nil {
		return nil
	}

	rawCookies := jar.Cookies(ctx, u)
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
func ExportJSON(ctx context.Context, jar core.CookieJar, u *url.URL) (string, error) {
	exported := Export(ctx, jar, u)
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
func Import(ctx context.Context, jar core.CookieJar, u *url.URL, cookies []Cookie) {
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

		//nolint:gosec // Reconstructing http.Cookie from imported Cookie model
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

	jar.SetCookies(ctx, u, httpCookies)
}

// ImportJSON deserializes a JSON cookie payload and imports it into jar for target u.
func ImportJSON(ctx context.Context, jar core.CookieJar, u *url.URL, jsonStr string) error {
	if jar == nil || u == nil || jsonStr == "" || jsonStr == "[]" {
		return nil
	}

	var cookies []Cookie
	if err := json.Unmarshal(bytesconv.S2B(jsonStr), &cookies); err != nil {
		return err
	}

	Import(ctx, jar, u, cookies)

	return nil
}

// SortForBrowser sorts cookies in-place per RFC 6265 §5.4.
func SortForBrowser(cookies []*http.Cookie) {
	if len(cookies) <= 1 {
		return
	}

	slices.SortStableFunc(cookies, func(a, b *http.Cookie) int {
		// RFC 6265 Section 5.4 Step 2: Missing secondary sort by creation-time for equal-length paths.
		return len(b.Path) - len(a.Path)
	})
}

// BuildCookieHeader constructs an RFC 6265 compliant Cookie request header string.
func BuildCookieHeader(cookies []*http.Cookie) string {
	if len(cookies) == 0 {
		return ""
	}

	var (
		stackBuf [16]*http.Cookie
		sorted   []*http.Cookie
	)

	if len(cookies) <= len(stackBuf) {
		sorted = stackBuf[:len(cookies)]
		copy(sorted, cookies)
	} else {
		sorted = slices.Clone(cookies)
	}

	SortForBrowser(sorted)

	var sb strings.Builder
	sb.Grow(len(sorted) * 36)

	for i, c := range sorted {
		if i > 0 {
			sb.WriteString("; ")
		}

		sb.WriteString(c.Name)
		sb.WriteByte('=')
		sb.WriteString(c.Value)
	}

	return sb.String()
}

// ExportNetscape exports cookies formatted as a standard Netscape HTTP Cookie File (cookies.txt).
func ExportNetscape(ctx context.Context, jar core.CookieJar, u *url.URL) string {
	if jar == nil || u == nil {
		return ""
	}

	cookies := jar.Cookies(ctx, u)
	if len(cookies) == 0 {
		return ""
	}

	defaultHost := u.Hostname()

	var sb strings.Builder
	sb.Grow(len(cookies) * 80)
	sb.WriteString("# Netscape HTTP Cookie File\n\n")

	var numBuf [20]byte

	for _, c := range cookies {
		domain := generic.Coalesce(c.Domain, defaultHost)
		includeSubdomains := generic.Ternary(len(domain) > 0 && domain[0] == '.', "TRUE", "FALSE")
		path := generic.Coalesce(c.Path, "/")
		secure := generic.Ternary(c.Secure, "TRUE", "FALSE")

		expires := "0"
		if !c.Expires.IsZero() {
			b := strconv.AppendInt(numBuf[:0], c.Expires.Unix(), 10)
			expires = bytesconv.B2S(b)
		}

		sb.WriteString(domain)
		sb.WriteByte('\t')
		sb.WriteString(includeSubdomains)
		sb.WriteByte('\t')
		sb.WriteString(path)
		sb.WriteByte('\t')
		sb.WriteString(secure)
		sb.WriteByte('\t')
		sb.WriteString(expires)
		sb.WriteByte('\t')
		sb.WriteString(c.Name)
		sb.WriteByte('\t')
		sb.WriteString(c.Value)
		sb.WriteByte('\n')
	}

	return sb.String()
}

// ParseSingleCookie parses key and value bytes into an http.Cookie pointer.
func ParseSingleCookie(_, value []byte) *http.Cookie {
	header := http.Header{}
	header.Add("Set-Cookie", bytesconv.B2S(value))

	fakeResp := &http.Response{Header: header}

	parsed := fakeResp.Cookies()
	if len(parsed) > 0 {
		return parsed[0]
	}

	return nil
}
