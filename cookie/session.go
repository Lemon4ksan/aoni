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
)

// Cookie holds the data for a cookie in a JSON-serializable structure
// compatible with standard browser automation tools.
type Cookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires"`
	HTTPOnly bool      `json:"httpOnly"`
	Secure   bool      `json:"secure"`
}

// Mirror copies cookies with the specified names from the sourceURL to all targetURLs within a single jar.
func Mirror(jar http.CookieJar, sourceURL *url.URL, targetURLs []*url.URL, cookieNames ...string) {
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
		jar.SetCookies(target, toMirror)
	}
}

// Export prepares cookies for loading into Playwright, Chromedp, or other automation engines.
func Export(jar http.CookieJar, u *url.URL) []Cookie {
	if jar == nil || u == nil {
		return nil
	}

	var exported []Cookie
	for _, cookie := range jar.Cookies(u) {
		exported = append(exported, Cookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Expires:  cookie.Expires,
			HTTPOnly: cookie.HttpOnly,
			Secure:   cookie.Secure,
		})
	}

	return exported
}

// ExportJSON serializes the exported cookies for the given URL directly into a JSON string.
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

// Import imports cookies from the browser/automation data into http.CookieJar.
func Import(jar http.CookieJar, u *url.URL, cookies []Cookie) {
	if jar == nil || u == nil {
		return
	}

	var httpCookies []*http.Cookie
	for _, c := range cookies {
		httpCookies = append(httpCookies, &http.Cookie{ //nolint:gosec
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  c.Expires,
			HttpOnly: c.HTTPOnly,
			Secure:   c.Secure,
		})
	}

	jar.SetCookies(u, httpCookies)
}

// ImportJSON deserializes cookies from a JSON string and imports them into http.CookieJar.
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
