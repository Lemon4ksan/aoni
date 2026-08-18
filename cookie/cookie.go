// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/lemon4ksan/aoni/foundation/bytesconv"
	impl "github.com/lemon4ksan/aoni/internal/cookie"
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
func ParseSetCookieHeader(headerVal, defaultDomain, defaultPath string) Cookie {
	dto := impl.ParseSetCookieHeader(headerVal, defaultDomain, defaultPath)

	return Cookie{
		Expires:      dto.Expires,
		Name:         dto.Name,
		Value:        dto.Value,
		Domain:       dto.Domain,
		Path:         dto.Path,
		SameSite:     dto.SameSite,
		PartitionKey: dto.PartitionKey,
		HTTPOnly:     dto.HTTPOnly,
		Secure:       dto.Secure,
		Partitioned:  dto.Partitioned,
		MaxAge:       dto.MaxAge,
	}
}

// PathMatch reports whether reqPath matches cookiePath according to RFC 6265 §5.1.4.
func PathMatch(reqPath, cookiePath string) bool {
	return impl.PathMatch(reqPath, cookiePath)
}

// FilterForRequest filters a slice of cookies, returning only those matching destination u per RFC 6265 §5.1.4.
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
func Mirror(jar http.CookieJar, sourceURL *url.URL, targetURLs []*url.URL, cookieNames ...string) {
	if jar == nil || sourceURL == nil || len(targetURLs) == 0 || len(cookieNames) == 0 {
		return
	}

	cookies := jar.Cookies(sourceURL)
	if len(cookies) == 0 {
		return
	}

	toMirror := make([]*http.Cookie, 0, len(cookieNames))
	if len(cookieNames) > 8 {
		nameMap := make(map[string]struct{}, len(cookieNames))
		for _, name := range cookieNames {
			nameMap[name] = struct{}{}
		}

		for _, c := range cookies {
			if _, ok := nameMap[c.Name]; ok {
				toMirror = append(toMirror, c)
			}
		}
	} else {
		for _, c := range cookies {
			if slices.Contains(cookieNames, c.Name) {
				toMirror = append(toMirror, c)
			}
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
func SortForBrowser(cookies []*http.Cookie) {
	impl.SortForBrowser(cookies)
}

// BuildCookieHeader constructs an RFC 6265 compliant 'Cookie' request header string.
func BuildCookieHeader(cookies []*http.Cookie) string {
	return impl.BuildCookieHeader(cookies)
}

// ExportNetscape exports cookies formatted as a standard Netscape HTTP Cookie File (cookies.txt).
func ExportNetscape(jar http.CookieJar, u *url.URL) string {
	if jar == nil || u == nil {
		return ""
	}

	return impl.ExportNetscape(jar.Cookies(u), u.Hostname())
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
		sameSiteStr := ""
		switch c.SameSite {
		case http.SameSiteLaxMode:
			sameSiteStr = "Lax"
		case http.SameSiteStrictMode:
			sameSiteStr = "Strict"
		case http.SameSiteNoneMode:
			sameSiteStr = "None"
		}

		exported[i] = Cookie{
			Name:        c.Name,
			Value:       c.Value,
			Domain:      c.Domain,
			Path:        c.Path,
			Expires:     c.Expires,
			HTTPOnly:    c.HttpOnly,
			Secure:      c.Secure,
			MaxAge:      c.MaxAge,
			SameSite:    sameSiteStr,
			Partitioned: c.Partitioned,
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
		var sameSite http.SameSite
		switch c.SameSite {
		case "Lax":
			sameSite = http.SameSiteLaxMode
		case "Strict":
			sameSite = http.SameSiteStrictMode
		case "None":
			sameSite = http.SameSiteNoneMode
		}

		httpCookies[i] = &http.Cookie{ //nolint:gosec
			Name:        c.Name,
			Value:       c.Value,
			Domain:      c.Domain,
			Path:        c.Path,
			Expires:     c.Expires,
			HttpOnly:    c.HTTPOnly,
			Secure:      c.Secure,
			MaxAge:      c.MaxAge,
			SameSite:    sameSite,
			Partitioned: c.Partitioned,
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
