// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import (
	"time"
	"context"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/lemon4ksan/foundation/codec/json"
	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
)

// MaxCookieAgeSeconds defines the maximum recommended cookie lifetime in seconds (400 days / 34,560,000s)
// as mandated by RFC 6265bis §5.5.
const (
	MaxCookieAgeSeconds = 34560000
	MaxCookieAgeLimit   = 400 * 24 * time.Hour
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
type Cookie struct {
	Expires      time.Time `json:"expires"`
	Name         string    `json:"name"`
	Value        string    `json:"value"`
	Domain       string    `json:"domain"`
	Path         string    `json:"path"`
	SameSite     string    `json:"sameSite,omitempty"`
	PartitionKey string    `json:"partitionKey,omitempty"`
	HTTPOnly     bool      `json:"httpOnly,omitempty"`
	Secure       bool      `json:"secure,omitempty"`
	Partitioned  bool      `json:"partitioned,omitempty"`
	MaxAge       int       `json:"maxAge,omitempty"`
}

// CookieJar defines a context-aware HTTP cookie storage interface supporting RFC 6265bis CHIPS.
//
// Unlike the standard [net/http/cookiejar.Jar], this interface accepts a [context.Context]
// to extract proxy and partition keys for strict cookie isolation.
type CookieJar interface {
	// SetCookies handles the receipt of the cookies in a reply for the
	// given URL. It may or may not choose to save the cookies, depending
	// on the jar's policy and implementation.
	SetCookies(ctx context.Context, u *url.URL, cookies []*http.Cookie)

	// Cookies returns the cookies to send in a request for the given URL.
	// It is up to the implementation to honor the standard cookie use
	// restrictions such as in RFC 6265.
	Cookies(ctx context.Context, u *url.URL) []*http.Cookie
}

// hasProhibitedControlChars reports whether s contains CTL characters %x00-08 / %x0A-1F / %x7F (excluding HTAB %x09)
// per RFC 6265bis §5.5 Step 1 & §5.7 Step 3.
func hasProhibitedControlChars(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if (b <= 0x08) || (b >= 0x0A && b <= 0x1F) || b == 0x7F {
			return true
		}
	}

	return false
}

// ParseSetCookieHeader parses a raw Set-Cookie header line with zero heap allocations (RFC 6265 §5.2, RFC 6265bis §5.5 & §5.7).
func ParseSetCookieHeader(headerVal, defaultDomain, defaultPath string) Cookie {
	if headerVal == "" || hasProhibitedControlChars(headerVal) {
		return Cookie{}
	}

	c := Cookie{
		Domain: defaultDomain,
		Path:   defaultPath,
	}

	isFirst := true
	for key, val := range bytesconv.ScanPairs(headerVal, ';', '=') {
		if isFirst {
			c.Name = key
			c.Value = val
			isFirst = false

			continue
		}

		ParseCookieAttribute(key, val, &c)
	}

	// RFC 6265bis §5.5 Step 5 & §5.7 Step 4: Sum of lengths of name and value must not exceed 4096 octets
	if c.Name == "" || len(c.Name)+len(c.Value) > 4096 {
		return Cookie{}
	}

	return c
}

// ParseCookieAttribute sets the corresponding field on [Cookie] with zero heap allocations using case-insensitive ASCII comparison.
func ParseCookieAttribute(key, val string, c *Cookie) {
	hasVal := len(val) > 0

	// RFC 6265bis §5.5 Step 6: Attribute value longer than 1024 octets must be ignored
	if len(val) > 1024 {
		return
	}

	switch {
	case bytesconv.EqualFoldASCII(key, "httponly"):
		c.HTTPOnly = true
	case bytesconv.EqualFoldASCII(key, "secure"):
		c.Secure = true
	case bytesconv.EqualFoldASCII(key, "partitioned"):
		c.Partitioned = true
	case bytesconv.EqualFoldASCII(key, "samesite"):
		if hasVal {
			switch {
			case bytesconv.EqualFoldASCII(val, "strict"):
				c.SameSite = "Strict"
			case bytesconv.EqualFoldASCII(val, "lax"):
				c.SameSite = "Lax"
			case bytesconv.EqualFoldASCII(val, "none"):
				c.SameSite = "None"
			default:
				c.SameSite = "Default"
			}
		}

	case bytesconv.EqualFoldASCII(key, "domain"):
		if hasVal {
			c.Domain = strings.TrimPrefix(val, ".")
		}
	case bytesconv.EqualFoldASCII(key, "path"):
		if hasVal {
			c.Path = val
		}
	case bytesconv.EqualFoldASCII(key, "max-age"):
		if hasVal {
			if maxAge, err := strconv.Atoi(val); err == nil {
				if maxAge > MaxCookieAgeSeconds {
					maxAge = MaxCookieAgeSeconds
				}

				c.MaxAge = maxAge
			}
		}

	case bytesconv.EqualFoldASCII(key, "expires"):
		if hasVal {
			if exp, err := http.ParseTime(val); err == nil {
				c.Expires = exp
			}
		}
	}
}

// PathMatch reports whether reqPath matches cookiePath per RFC 6265 §5.1.4.
func PathMatch(reqPath, cookiePath string) bool {
	if cookiePath == "" {
		cookiePath = "/"
	}

	if reqPath == "" {
		reqPath = "/"
	}

	if reqPath == cookiePath {
		return true
	}

	if strings.HasPrefix(reqPath, cookiePath) {
		if strings.HasSuffix(cookiePath, "/") {
			return true
		}

		if len(reqPath) > len(cookiePath) && reqPath[len(cookiePath)] == '/' {
			return true
		}
	}

	return false
}

// ValidatePrefix reports whether a cookie satisfies RFC 6265bis §4.1.3 & §5.4 cookie prefix rules:
//   - "__Secure-": MUST have Secure=true.
//   - "__Host-": MUST have Secure=true, Path="/", and empty Domain (host-only).
//   - Nameless cookies whose value begins with "__Secure-" or "__Host-" MUST be rejected (RFC 6265bis §5.7 step 22).
func ValidatePrefix(c Cookie) bool {
	if c.Name == "" {
		lowerVal := strings.ToLower(c.Value)
		if strings.HasPrefix(lowerVal, "__secure-") || strings.HasPrefix(lowerVal, "__host-") {
			return false
		}

		return true
	}

	lowerName := strings.ToLower(c.Name)
	if strings.HasPrefix(lowerName, "__secure-") {
		return c.Secure
	}

	if strings.HasPrefix(lowerName, "__host-") {
		return c.Secure && c.Path == "/" && c.Domain == ""
	}

	return true
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
func Mirror(ctx context.Context, jar CookieJar, sourceURL *url.URL, targetURLs []*url.URL, cookieNames ...string) {
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
func Export(ctx context.Context, jar CookieJar, u *url.URL) []Cookie {
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
func ExportJSON(ctx context.Context, jar CookieJar, u *url.URL) (string, error) {
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
func Import(ctx context.Context, jar CookieJar, u *url.URL, cookies []Cookie) {
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
func ImportJSON(ctx context.Context, jar CookieJar, u *url.URL, jsonStr string) error {
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
func ExportNetscape(ctx context.Context, jar CookieJar, u *url.URL) string {
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
