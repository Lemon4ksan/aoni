// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cookie provides proxy-isolated cookie management and session persistence utilities.
package cookie

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/sync/keylock"
	"golang.org/x/net/publicsuffix"
)

type proxyCtxKey struct{}

// WithProxyAddress returns a new context carrying the active proxy URL string.
func WithProxyAddress(ctx context.Context, addr string) context.Context {
	return context.WithValue(ctx, proxyCtxKey{}, addr)
}

// GetProxyAddress extracts the active proxy URL string from context.
// Returns an empty string if no proxy address is attached.
func GetProxyAddress(ctx context.Context) string {
	if val := ctx.Value(proxyCtxKey{}); val != nil {
		if addr, ok := val.(string); ok {
			return addr
		}
	}

	return ""
}

// ProxyIsolatedJar provides per-proxy cookie storage isolation.
// Cookies set or read for a specific proxy exit node are stored in an independent [http.CookieJar],
// preventing session correlation and cookie leakage across different exit nodes.
//
// Thread Safety:
// Safe for concurrent access across multiple goroutines.
type ProxyIsolatedJar struct {
	mu      sync.RWMutex
	jars    map[string]http.CookieJar
	km      keylock.KeyMutex[string]
	backend Storage
}

// NewProxyIsolatedJar creates an uninitialized [ProxyIsolatedJar].
func NewProxyIsolatedJar() *ProxyIsolatedJar {
	return &ProxyIsolatedJar{
		jars: make(map[string]http.CookieJar),
	}
}

// SetCookies satisfies [http.CookieJar].
// Delegates to the default (empty key) cookie jar when invoked without request context.
func (p *ProxyIsolatedJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if jar := p.GetJarForProxy(""); jar != nil {
		jar.SetCookies(u, cookies)
	}
}

// Cookies satisfies [http.CookieJar].
// Returns cookies from the default (empty key) jar when invoked without request context.
func (p *ProxyIsolatedJar) Cookies(u *url.URL) []*http.Cookie {
	if jar := p.GetJarForProxy(""); jar != nil {
		return jar.Cookies(u)
	}

	return nil
}

// GetJarForProxy retrieves or initializes the isolated [http.CookieJar] for the target proxy URL.
func (p *ProxyIsolatedJar) GetJarForProxy(proxyURL string) http.CookieJar {
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
	backend := p.backend
	p.mu.RUnlock()

	if ok {
		return jar
	}

	baseJar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	if err != nil {
		return nil
	}

	if backend != nil {
		jar = p.initPersistentJar(proxyURL, baseJar, backend)
	} else {
		jar = baseJar
	}

	p.mu.Lock()
	p.jars[proxyURL] = jar
	p.mu.Unlock()

	return jar
}

// WithStorageBackend configures persistent storage for cookie jars.
func (p *ProxyIsolatedJar) WithStorageBackend(backend Storage) *ProxyIsolatedJar {
	p.mu.Lock()
	p.backend = backend
	p.mu.Unlock()

	return p
}

// CookiesForProxy retrieves cookies associated with a specific proxy URL and target URL.
func (p *ProxyIsolatedJar) CookiesForProxy(proxyURL string, u *url.URL) []*http.Cookie {
	if jar := p.GetJarForProxy(proxyURL); jar != nil {
		return jar.Cookies(u)
	}

	return nil
}

// SetCookiesForProxy manually stores cookies for a specific proxy URL and target URL.
func (p *ProxyIsolatedJar) SetCookiesForProxy(proxyURL string, u *url.URL, cookies []*http.Cookie) {
	if jar := p.GetJarForProxy(proxyURL); jar != nil {
		jar.SetCookies(u, cookies)
	}
}

// GetJar extracts the active proxy URL from context and yields the corresponding isolated jar.
func (p *ProxyIsolatedJar) GetJar(ctx context.Context) http.CookieJar {
	return p.GetJarForProxy(GetProxyAddress(ctx))
}

type cookieKey struct {
	domain string
	path   string
	name   string
}

func (p *ProxyIsolatedJar) initPersistentJar(proxyURL string, baseJar http.CookieJar, backend Storage) http.CookieJar {
	pJar := &PersistentJar{
		CookieJar:  baseJar,
		proxyURL:   proxyURL,
		backend:    backend,
		cookiesMap: make(map[cookieKey]Cookie),
	}

	cookies, err := backend.Load(proxyURL)
	if err != nil || len(cookies) == 0 {
		return pJar
	}

	pJar.mu.Lock()
	defer pJar.mu.Unlock()

	for _, c := range cookies {
		key := cookieKey{domain: c.Domain, path: c.Path, name: c.Name}
		pJar.cookiesMap[key] = c

		scheme := "http"
		if c.Secure {
			scheme = "https"
		}

		domain := strings.TrimPrefix(c.Domain, ".")

		u, parseErr := url.Parse(scheme + "://" + domain + c.Path)
		if parseErr == nil {
			baseJar.SetCookies(u, []*http.Cookie{
				{
					Name:     c.Name,
					Value:    c.Value,
					Domain:   c.Domain,
					Path:     c.Path,
					Expires:  c.Expires,
					HttpOnly: c.HTTPOnly,
					Secure:   c.Secure,
				},
			})
		}
	}

	return pJar
}

// PersistentJar decorates an [http.CookieJar] to sync cookie updates to a [Storage] backend.
type PersistentJar struct {
	http.CookieJar
	proxyURL   string
	backend    Storage
	mu         sync.Mutex
	cookiesMap map[cookieKey]Cookie
}

// SetCookies stores cookies in the inner jar and syncs non-expired cookies to persistent storage.
func (pj *PersistentJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	pj.CookieJar.SetCookies(u, cookies)

	pj.mu.Lock()
	now := time.Now()
	changed := false

	for _, c := range cookies {
		key := cookieKey{domain: c.Domain, path: c.Path, name: c.Name}
		if !c.Expires.IsZero() && c.Expires.Before(now) {
			if _, exists := pj.cookiesMap[key]; exists {
				delete(pj.cookiesMap, key)

				changed = true
			}

			continue
		}

		domain := c.Domain
		if domain == "" {
			domain = u.Hostname()
		}

		path := c.Path
		if path == "" {
			path = "/"
		}

		key = cookieKey{domain: domain, path: path, name: c.Name}
		pj.cookiesMap[key] = Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   domain,
			Path:     path,
			Expires:  c.Expires,
			HTTPOnly: c.HttpOnly,
			Secure:   c.Secure,
		}
		changed = true
	}

	for k, c := range pj.cookiesMap {
		if !c.Expires.IsZero() && c.Expires.Before(now) {
			delete(pj.cookiesMap, k)

			changed = true
		}
	}

	var list []Cookie
	if changed {
		list = make([]Cookie, 0, len(pj.cookiesMap))
		for _, c := range pj.cookiesMap {
			list = append(list, c)
		}
	}

	pj.mu.Unlock()

	if changed {
		_ = pj.backend.Save(pj.proxyURL, list)
	}
}
