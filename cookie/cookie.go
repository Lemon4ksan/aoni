// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cookie provides proxy-isolated cookie management, persistence, and transport decoration.
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

// Cookie represents a browser cookie structure formatted for JSON persistence and external automation tools.
type Cookie struct {
	Expires  time.Time `json:"expires"`
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	HTTPOnly bool      `json:"httpOnly"`
	Secure   bool      `json:"secure"`
}

// PathMatch reports whether requestPath matches cookiePath according to RFC 6265 Section 5.1.4.
//
// Postconditions:
//   - Correctly handles sub-paths requiring explicit slash boundaries (e.g., "/api" matches "/api/v1" but NOT "/api-v2").
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

// FilterForRequest filters cookies matching destination u using RFC 6265 path-matching rules.
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

// Mirror copies matching cookies from sourceURL to each targetURL in jar.
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

// SortForBrowser sorts cookies according to RFC 6265 Section 5.4 (longest path first).
func SortForBrowser(cookies []*http.Cookie) {
	if len(cookies) <= 1 {
		return
	}

	slices.SortStableFunc(cookies, func(a, b *http.Cookie) int {
		return len(b.Path) - len(a.Path)
	})
}

// BuildCookieHeader constructs an RFC 6265 compliant 'Cookie' header string.
//
// Sorts cookies according to path length precedence without mutating the original slice.
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
