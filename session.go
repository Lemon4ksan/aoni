// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
)

// ProxyAwareSessionCache wraps the uTLS [utls.ClientSessionCache] and automatically
// invalidates cached TLS session tickets when the active proxy or source IP changes.
// This prevents server-side tracking of a client across different exit IPs
// via session ticket correlation.
type ProxyAwareSessionCache struct {
	mu         sync.RWMutex
	inner      utls.ClientSessionCache
	currentKey string
}

// NewProxyAwareSessionCache creates a new [ProxyAwareSessionCache].
func NewProxyAwareSessionCache() *ProxyAwareSessionCache {
	return &ProxyAwareSessionCache{
		inner: utls.NewLRUClientSessionCache(256),
	}
}

// Get retrieves a cached session for the given server name.
// If the session was cached under a different proxy key, it returns nil
// to force a fresh handshake.
func (c *ProxyAwareSessionCache) Get(serverName string) (*utls.ClientSessionState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.inner != nil {
		return c.inner.Get(serverName)
	}

	return nil, false
}

// Put stores a TLS session ticket.
func (c *ProxyAwareSessionCache) Put(serverName string, session *utls.ClientSessionState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.inner != nil {
		c.inner.Put(serverName, session)
	}
}

// SetProxyKey invalidates all cached sessions and starts a fresh session cache
// for the given proxy key (typically the proxy address or source IP).
// This ensures that when the proxy changes, no session tickets from the
// previous proxy are reused, preventing session correlation tracking.
func (c *ProxyAwareSessionCache) SetProxyKey(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.currentKey == key {
		return
	}

	// Discard the old cache entirely and start fresh.
	c.inner = utls.NewLRUClientSessionCache(256)
	c.currentKey = key
}

// CurrentProxyKey returns the currently active proxy key.
func (c *ProxyAwareSessionCache) CurrentProxyKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentKey
}

// Clear manually flushes all currently cached TLS sessions.
func (c *ProxyAwareSessionCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.inner = utls.NewLRUClientSessionCache(256)
}

// CookieData holds the data for a cookie in a JSON-serializable structure
// compatible with standard browser automation tools.
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

// ExportCookies prepares cookies for loading into Playwright, Chromedp, or other automation engines.
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

// ExportCookiesJSON serializes the exported cookies for the given URL directly into a JSON string.
func ExportCookiesJSON(jar http.CookieJar, u *url.URL) (string, error) {
	exported := ExportCookies(jar, u)
	if len(exported) == 0 {
		return "[]", nil
	}

	b, err := json.Marshal(exported)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// ImportCookies imports cookies from the browser/automation data into http.CookieJar.
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

// ImportCookiesJSON deserializes cookies from a JSON string and imports them into http.CookieJar.
func ImportCookiesJSON(jar http.CookieJar, u *url.URL, jsonStr string) error {
	if jar == nil || u == nil || jsonStr == "" || jsonStr == "[]" {
		return nil
	}

	var cookies []CookieData
	if err := json.Unmarshal([]byte(jsonStr), &cookies); err != nil {
		return err
	}

	ImportCookies(jar, u, cookies)

	return nil
}
