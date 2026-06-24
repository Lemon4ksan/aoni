// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"net/http"
	"net/url"
	"slices"
	"time"
)

// CookieData holds the data for a cookie.
type CookieData struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires"`
	HTTPOnly bool      `json:"httpOnly"`
	Secure   bool      `json:"secure"`
}

// MirrorCookies copies cookies with the specified names from the sourceURL to all targetURLs within a single jar.
func MirrorCookies(jar http.CookieJar, sourceURL *url.URL, targetURLs []*url.URL, cookieNames ...string) {
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

// ExportCookies prepares cookies for loading into Playwright/Chromedp.
func ExportCookies(jar http.CookieJar, u *url.URL) []CookieData {
	if jar == nil || u == nil {
		return nil
	}

	var exported []CookieData
	for _, cookie := range jar.Cookies(u) {
		exported = append(exported, CookieData{
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

// ImportCookies imports cookies from the browser into aoni.CookieJar.
func ImportCookies(jar http.CookieJar, u *url.URL, cookies []CookieData) {
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
