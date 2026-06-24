// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"runtime"
	"sync"

	"github.com/lemon4ksan/miyako/sync/keylock"
)

type proxyCtxKey struct{}

// ProxyIsolatedCookieJar is an isolated cookie jar that stores cookies per proxy URL.
// It is safe for concurrent use by multiple goroutines.
type ProxyIsolatedCookieJar struct {
	mu   sync.RWMutex
	jars map[string]http.CookieJar
	km   keylock.KeyMutex[string]

	// goroutineProxies stores per-goroutine active proxy URLs.
	// Key: goroutine ID (string), Value: proxy URL string.
	// This eliminates the race condition of a single shared activeProxy field.
	goroutineProxies sync.Map
}

// NewProxyIsolatedCookieJar creates a new ProxyIsolatedCookieJar.
func NewProxyIsolatedCookieJar() *ProxyIsolatedCookieJar {
	return &ProxyIsolatedCookieJar{
		jars: make(map[string]http.CookieJar),
	}
}

// goroutineID returns a unique identifier for the current goroutine.
func goroutineID() string {
	var buf [64]byte

	n := runtime.Stack(buf[:], false)

	// Stack format: "goroutine 18 [running]:\n..."
	// Extract the number after "goroutine ".
	id := buf[len("goroutine ") : n-3]

	return string(id)
}

// SetActiveProxy sets the proxy URL used for SetCookies/Cookies fallback calls
// (e.g. during http.Client redirects where context is unavailable).
// Safe for concurrent use: each goroutine's proxy is stored independently.
// The caller MUST call ClearActiveProxy after the request completes.
func (p *ProxyIsolatedCookieJar) SetActiveProxy(proxyURL string) {
	p.goroutineProxies.Store(goroutineID(), proxyURL)
}

// ClearActiveProxy clears the active proxy for the current goroutine.
func (p *ProxyIsolatedCookieJar) ClearActiveProxy() {
	p.goroutineProxies.Delete(goroutineID())
}

// SetCookies implements the http.CookieJar interface.
// Used as fallback when http.Client calls during redirects have no context.
func (p *ProxyIsolatedCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	proxy := p.activeProxyForCurrentGoroutine()

	jar := p.getJarByProxy(proxy)
	if jar != nil {
		jar.SetCookies(u, cookies)
	}
}

// Cookies implements the http.CookieJar interface.
// Used as fallback when http.Client calls during redirects have no context.
func (p *ProxyIsolatedCookieJar) Cookies(u *url.URL) []*http.Cookie {
	proxy := p.activeProxyForCurrentGoroutine()

	jar := p.getJarByProxy(proxy)
	if jar != nil {
		return jar.Cookies(u)
	}

	return nil
}

func (p *ProxyIsolatedCookieJar) activeProxyForCurrentGoroutine() string {
	val, ok := p.goroutineProxies.Load(goroutineID())
	if !ok {
		return ""
	}

	return val.(string)
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
