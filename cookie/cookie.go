// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
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
	Expires      time.Time `json:"expires,omitempty"`
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

// ParseSetCookieHeader parses a raw 'Set-Cookie' header line into a structured [Cookie].
//
// Specification Adherence:
// Conforms to RFC 6265 §5.2 for attribute parsing and RFC 6265bis for CHIPS 'Partitioned' directives.
//
// Preconditions:
//   - headerVal must contain a valid key-value pair separated by '='; otherwise an empty [Cookie] is returned.
//   - defaultDomain and defaultPath are applied if the header omits explicit 'Domain=' or 'Path=' attributes.
//
// Postconditions:
//   - Returns a populated [Cookie] value with normalized domain names (leading dot stripped per RFC 6265 §5.2.3).
func ParseSetCookieHeader(headerVal, defaultDomain, defaultPath string) Cookie {
	if headerVal == "" {
		return Cookie{}
	}

	parts := strings.Split(headerVal, ";")
	if len(parts) == 0 {
		return Cookie{}
	}

	nameVal := strings.TrimSpace(parts[0])

	eqIdx := strings.IndexByte(nameVal, '=')
	if eqIdx <= 0 {
		return Cookie{}
	}

	c := Cookie{
		Name:   strings.TrimSpace(nameVal[:eqIdx]),
		Value:  strings.TrimSpace(nameVal[eqIdx+1:]),
		Domain: defaultDomain,
		Path:   defaultPath,
	}

	for _, attr := range parts[1:] {
		parseCookieAttribute(strings.TrimSpace(attr), &c)
	}

	return c
}

func parseCookieAttribute(attr string, c *Cookie) {
	if attr == "" {
		return
	}

	lower := strings.ToLower(attr)
	switch {
	case lower == "httponly":
		c.HTTPOnly = true
	case lower == "secure":
		c.Secure = true
	case lower == "partitioned":
		c.Partitioned = true
	case strings.HasPrefix(lower, "samesite="):
		c.SameSite = parseAttributeValue(attr)
	case strings.HasPrefix(lower, "domain="):
		c.Domain = strings.TrimPrefix(parseAttributeValue(attr), ".")
	case strings.HasPrefix(lower, "path="):
		c.Path = parseAttributeValue(attr)
	case strings.HasPrefix(lower, "max-age="):
		if maxAge, err := strconv.Atoi(parseAttributeValue(attr)); err == nil {
			c.MaxAge = maxAge
		}
	case strings.HasPrefix(lower, "expires="):
		if exp, err := http.ParseTime(parseAttributeValue(attr)); err == nil {
			c.Expires = exp
		}
	}
}

func parseAttributeValue(attr string) string {
	_, val, ok := strings.Cut(attr, "=")
	if !ok {
		return ""
	}

	return strings.TrimSpace(val)
}

// PathMatch reports whether reqPath matches cookiePath according to RFC 6265 §5.1.4.
//
// Specification Adherence:
// Follows RFC 6265 §5.1.4 path-matching rules (exact match, prefix match with trailing slash, or prefix match before path boundary slash).
//
// Preconditions:
//   - If reqPath or cookiePath are empty strings, they default to "/".
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

// FilterForRequest filters a slice of cookies, returning only those matching destination u per RFC 6265 §5.1.4.
//
// Preconditions:
//   - Returns nil if cookies is empty or u is nil.
//
// Postconditions:
//   - Yields a fresh slice containing pointers to matching cookies without mutating input slices.
func FilterForRequest(cookies []*http.Cookie, u *url.URL) []*http.Cookie {
	if len(cookies) == 0 || u == nil {
		return nil
	}

	reqPath := u.Path
	if reqPath == "" {
		reqPath = "/"
	}

	filtered := make([]*http.Cookie, 0, len(cookies))
	for _, c := range cookies {
		if PathMatch(reqPath, c.Path) {
			filtered = append(filtered, c)
		}
	}

	return filtered
}

// Mirror copies specified cookies by name from sourceURL to each destination URL in targetURLs inside jar.
//
// Preconditions:
//   - Returns immediately with no operation if jar, sourceURL, targetURLs, or cookieNames are empty/nil.
//
// Postconditions:
//   - Target URLs in jar receive duplicate cookie entries carrying identical values and path metadata.
func Mirror(jar http.CookieJar, sourceURL *url.URL, targetURLs []*url.URL, cookieNames ...string) {
	if jar == nil || sourceURL == nil || len(targetURLs) == 0 || len(cookieNames) == 0 {
		return
	}

	cookies := jar.Cookies(sourceURL)
	if len(cookies) == 0 {
		return
	}

	toMirror := make([]*http.Cookie, 0, len(cookieNames))
	for _, c := range cookies {
		if slices.Contains(cookieNames, c.Name) {
			toMirror = append(toMirror, c)
		}
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
//
// Specification Adherence:
// Conforms to RFC 6265 §5.4 requirement: cookies with longer paths MUST precede cookies with shorter paths.
//
// Postconditions:
//   - Uses stable sort (`slices.SortStableFunc`) to preserve original creation ordering for cookies with equal path lengths.
func SortForBrowser(cookies []*http.Cookie) {
	if len(cookies) <= 1 {
		return
	}

	slices.SortStableFunc(cookies, func(a, b *http.Cookie) int {
		return len(b.Path) - len(a.Path)
	})
}

// BuildCookieHeader constructs an RFC 6265 compliant 'Cookie' request header string.
//
// Specification Adherence:
// Sorts cookies by path length per RFC 6265 §5.4 prior to formatting.
//
// Memory Allocation Fast-Path:
// Uses a 16-element stack array for typical request headers, avoiding heap allocations for standard cookie lists.
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
func ExportNetscape(jar http.CookieJar, u *url.URL) string {
	if jar == nil || u == nil {
		return ""
	}

	cookies := jar.Cookies(u)
	if len(cookies) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.Grow(len(cookies) * 80)
	sb.WriteString("# Netscape HTTP Cookie File\n\n")

	var numBuf [20]byte

	for _, c := range cookies {
		domain := c.Domain
		if domain == "" {
			domain = u.Hostname()
		}

		includeSubdomains := "FALSE"
		if len(domain) > 0 && domain[0] == '.' {
			includeSubdomains = "TRUE"
		}

		path := c.Path
		if path == "" {
			path = "/"
		}

		secure := "FALSE"
		if c.Secure {
			secure = "TRUE"
		}

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

// Export converts cookies for u from jar into exported [Cookie] structures.
func Export(jar http.CookieJar, u *url.URL) []Cookie {
	if jar == nil || u == nil {
		return nil
	}

	rawCookies := jar.Cookies(u)
	if len(rawCookies) == 0 {
		return nil
	}

	exported := make([]Cookie, len(rawCookies))
	for i, c := range rawCookies {
		exported[i] = Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  c.Expires,
			HTTPOnly: c.HttpOnly,
			Secure:   c.Secure,
			MaxAge:   c.MaxAge,
		}
	}

	return exported
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

	httpCookies := make([]*http.Cookie, len(cookies))
	for i, c := range cookies {
		httpCookies[i] = &http.Cookie{ //nolint:gosec
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  c.Expires,
			HttpOnly: c.HTTPOnly,
			Secure:   c.Secure,
			MaxAge:   c.MaxAge,
		}
	}

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
