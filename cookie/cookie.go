// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cookie provides proxy-isolated cookie management, session persistence, and transport decoration.
package cookie

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Cookie represents a browser cookie structure formatted for JSON persistence and external automation tools.
type Cookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires"`
	HTTPOnly bool      `json:"httpOnly"`
	Secure   bool      `json:"secure"`
}

// Mirror copies matching cookies from sourceURL to each targetURL within jar.
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

// SortForBrowser sorts cookies according to RFC 6265 Section 5.4:
// longest path first, followed by creation order.
func SortForBrowser(cookies []*http.Cookie) {
	if len(cookies) <= 1 {
		return
	}

	slices.SortStableFunc(cookies, func(a, b *http.Cookie) int {
		return len(b.Path) - len(a.Path)
	})
}

// BuildCookieHeader constructs a single RFC-compliant "name1=val1; name2=val2" string
// using strings.Builder to minimize heap allocations.
func BuildCookieHeader(cookies []*http.Cookie) string {
	if len(cookies) == 0 {
		return ""
	}

	SortForBrowser(cookies)

	var sb strings.Builder
	// Preallocate estimated capacity (~32 bytes per cookie entry)
	sb.Grow(len(cookies) * 32)

	for i, c := range cookies {
		if i > 0 {
			sb.WriteString("; ")
		}

		sb.WriteString(c.Name)
		sb.WriteByte('=')
		sb.WriteString(c.Value)
	}

	return sb.String()
}

// ExportNetscape exports cookies formatted as standard Netscape HTTP Cookie File (cookies.txt).
func ExportNetscape(jar http.CookieJar, u *url.URL) string {
	if jar == nil || u == nil {
		return ""
	}

	cookies := jar.Cookies(u)
	if len(cookies) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Netscape HTTP Cookie File\n\n")

	for _, c := range cookies {
		domain := c.Domain
		if domain == "" {
			domain = u.Hostname()
		}

		includeSubdomains := "FALSE"
		if strings.HasPrefix(domain, ".") {
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
			expires = strconv.FormatInt(c.Expires.Unix(), 10)
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

// Export retrieves cookies for u from jar and converts them to a slice of Cookie structs.
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

// ExportJSON serializes exported cookies for u into a JSON formatted string.
func ExportJSON(jar http.CookieJar, u *url.URL) (string, error) {
	exported := Export(jar, u)
	if len(exported) == 0 {
		return "[]", nil
	}

	b, err := json.Marshal(exported)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// Import injects a slice of Cookie structs into jar for target u.
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

// ImportJSON deserializes a JSON string of cookies and imports them into jar for target u.
func ImportJSON(jar http.CookieJar, u *url.URL, jsonStr string) error {
	if jar == nil || u == nil || jsonStr == "" || jsonStr == "[]" {
		return nil
	}

	var cookies []Cookie
	if err := json.Unmarshal([]byte(jsonStr), &cookies); err != nil {
		return err
	}

	Import(jar, u, cookies)

	return nil
}
