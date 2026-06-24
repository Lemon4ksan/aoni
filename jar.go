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
type ProxyIsolatedCookieJar struct {
	mu          sync.RWMutex
	jars        map[string]http.CookieJar
	km          keylock.KeyMutex[string]
	activeProxy string
	hasActive   bool
}

// NewProxyIsolatedCookieJar creates a new ProxyIsolatedCookieJar.
func NewProxyIsolatedCookieJar() *ProxyIsolatedCookieJar {
	return &ProxyIsolatedCookieJar{
		jars: make(map[string]http.CookieJar),
	}
}

// SetActiveProxy sets the proxy URL used for SetCookies/Cookies fallback calls
// (e.g. during http.Client redirects where context is unavailable).
// The caller MUST call ClearActiveProxy after the request completes.
func (p *ProxyIsolatedCookieJar) SetActiveProxy(proxyURL string) {
	p.mu.Lock()
	p.activeProxy = proxyURL
	p.hasActive = true
	p.mu.Unlock()
}

// ClearActiveProxy clears the active proxy set by SetActiveProxy.
func (p *ProxyIsolatedCookieJar) ClearActiveProxy() {
	p.mu.Lock()
	p.activeProxy = ""
	p.hasActive = false
	p.mu.Unlock()
}

// SetCookies implements the http.CookieJar interface.
// Used as fallback when http.Client calls during redirects have no context.
func (p *ProxyIsolatedCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	p.mu.RLock()
	proxy := p.activeProxy
	active := p.hasActive
	p.mu.RUnlock()

	if !active {
		proxy = ""
	}

	jar := p.getJarByProxy(proxy)
	if jar != nil {
		jar.SetCookies(u, cookies)
	}
}

// Cookies implements the http.CookieJar interface.
// Used as fallback when http.Client calls during redirects have no context.
func (p *ProxyIsolatedCookieJar) Cookies(u *url.URL) []*http.Cookie {
	p.mu.RLock()
	proxy := p.activeProxy
	active := p.hasActive
	p.mu.RUnlock()

	if !active {
		proxy = ""
	}

	jar := p.getJarByProxy(proxy)
	if jar != nil {
		return jar.Cookies(u)
	}

	return nil
}

// getJar returns the cookie jar for the given context, creating it if necessary.
func (p *ProxyIsolatedCookieJar) getJar(ctx context.Context) http.CookieJar {
	proxyURL := ""
	if val := ctx.Value(proxyCtxKey{}); val != nil {
		proxyURL = val.(string)
	}

	return p.getJarByProxy(proxyURL)
}

func (p *ProxyIsolatedCookieJar) getJarByProxy(proxyURL string) http.CookieJar {
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
