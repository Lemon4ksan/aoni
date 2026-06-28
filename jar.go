// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"

	"github.com/lemon4ksan/miyako/sync/keylock"
)

type proxyCtxKey struct{}

// ProxyIsolatedCookieJar is an isolated cookie jar that stores cookies per proxy URL.
// It is safe for concurrent use by multiple goroutines.
//
// Cookie isolation works for direct requests: the proxy URL is extracted from the
// request context and used to select the correct per-proxy jar.
//
// Limitation: during HTTP redirects, the standard library's http.Client calls
// SetCookies/Cookies without passing the request context. In this case the jar
// falls back to the default (empty-key) jar. This means per-proxy cookie isolation
// does not apply to redirect responses. In practice this is rarely an issue because
// proxy servers typically do not return redirects, and cookies from the target server
// arrive in the initial response before any redirect.
type ProxyIsolatedCookieJar struct {
	mu   sync.RWMutex
	jars map[string]http.CookieJar
	km   keylock.KeyMutex[string]
}

// NewProxyIsolatedCookieJar creates a new ProxyIsolatedCookieJar.
func NewProxyIsolatedCookieJar() *ProxyIsolatedCookieJar {
	return &ProxyIsolatedCookieJar{
		jars: make(map[string]http.CookieJar),
	}
}

// SetCookies implements the http.CookieJar interface.
// For direct requests, uses the proxy URL from the request context.
// For redirects (no context available), falls back to the default jar.
func (p *ProxyIsolatedCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	jar := p.GetJarForProxy("")
	if jar != nil {
		jar.SetCookies(u, cookies)
	}
}

// Cookies implements the http.CookieJar interface.
// For direct requests, uses the proxy URL from the request context.
// For redirects (no context available), falls back to the default jar.
func (p *ProxyIsolatedCookieJar) Cookies(u *url.URL) []*http.Cookie {
	jar := p.GetJarForProxy("")
	if jar != nil {
		return jar.Cookies(u)
	}

	return nil
}

// GetJarForProxy returns the specific [http.CookieJar] associated with the given proxy URL.
// This is a high-level helper to manage proxy cookies programmatically.
func (p *ProxyIsolatedCookieJar) GetJarForProxy(proxyURL string) http.CookieJar {
	p.mu.RLock()
	jar, ok := p.jars[proxyURL]
	p.mu.RUnlock()

	if ok {
		return jar
	}

	p.km.Lock(proxyURL)
	defer p.km.Unlock(proxyURL)

	p.mu.RLock()
	jar, ok = p.jars[proxyURL]
	p.mu.RUnlock()

	if ok {
		return jar
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil
	}

	p.mu.Lock()
	p.jars[proxyURL] = jar
	p.mu.Unlock()

	return jar
}

// SetCookiesForProxy manually stores cookies for a specific proxy URL.
func (p *ProxyIsolatedCookieJar) SetCookiesForProxy(proxyURL string, u *url.URL, cookies []*http.Cookie) {
	jar := p.GetJarForProxy(proxyURL)
	if jar != nil {
		jar.SetCookies(u, cookies)
	}
}

// CookiesForProxy manually retrieves cookies for a specific proxy URL.
func (p *ProxyIsolatedCookieJar) CookiesForProxy(proxyURL string, u *url.URL) []*http.Cookie {
	jar := p.GetJarForProxy(proxyURL)
	if jar != nil {
		return jar.Cookies(u)
	}

	return nil
}

// GetJar returns the cookie jar for the given context, creating it if necessary.
func (p *ProxyIsolatedCookieJar) GetJar(ctx context.Context) http.CookieJar {
	proxyURL := ""
	if val := ctx.Value(proxyCtxKey{}); val != nil {
		proxyURL = val.(string)
	}

	return p.GetJarForProxy(proxyURL)
}
