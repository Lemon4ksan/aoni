// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/sync/keylock"
	"golang.org/x/net/publicsuffix"
)

type proxyCtxKey struct{}

// WithProxyAddress returns a new Context carrying the active proxy URL string.
func WithProxyAddress(ctx context.Context, addr string) context.Context {
	return context.WithValue(ctx, proxyCtxKey{}, addr)
}

// GetProxyAddress retrieves the active proxy URL string stored in the context.
// Returns an empty string if no proxy context is attached.
func GetProxyAddress(ctx context.Context) string {
	val, ok := ctx.Value(proxyCtxKey{}).(string)
	if !ok {
		return ""
	}

	return val
}

// ProxyIsolatedJar provides per-proxy cookie storage isolation.
// Cookies set or read for a specific proxy exit node are stored in an independent http.CookieJar,
// preventing session correlation and cookie leakage across different exit nodes.
type ProxyIsolatedJar struct {
	mu      sync.RWMutex
	jars    map[string]http.CookieJar
	km      keylock.KeyMutex[string]
	backend Storage
}

// NewProxyIsolatedJar creates an uninitialized [ProxyIsolatedJar] ready for concurrent use.
func NewProxyIsolatedJar() *ProxyIsolatedJar {
	return &ProxyIsolatedJar{
		jars: make(map[string]http.CookieJar),
	}
}

// SetCookies satisfies [http.CookieJar].
// Delegates to the default (empty key) cookie jar when invoked without a proxy context.
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

// WithStorageBackend configures persistent storage for cookie jars and returns p.
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

// StartJanitor launches a background goroutine that periodically purges expired cookies
// across all proxy jars. The worker terminates automatically when ctx is canceled.
func (p *ProxyIsolatedJar) StartJanitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.PurgeExpired()
			}
		}
	}()
}

// PurgeExpired removes all expired cookies across all active proxy jars
// to prevent memory growth during long-running tasks.
func (p *ProxyIsolatedJar) PurgeExpired() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, jar := range p.jars {
		if pJar, ok := jar.(*PersistentJar); ok {
			pJar.purgeExpired()
		}
	}
}

func (pj *PersistentJar) purgeExpired() {
	pj.mu.Lock()
	defer pj.mu.Unlock()

	now := time.Now()
	changed := false

	for k, c := range pj.cookiesMap {
		if (!c.Expires.IsZero() && c.Expires.Before(now)) || c.MaxAge < 0 {
			delete(pj.cookiesMap, k)

			changed = true
		}
	}

	if changed && pj.backend != nil {
		list := make([]Cookie, 0, len(pj.cookiesMap))
		for _, c := range pj.cookiesMap {
			list = append(list, c)
		}

		_ = pj.backend.Save(pj.proxyURL, list)
	}
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
				{ //nolint:gosec
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

// PersistentJar decorates an [http.CookieJar] to automatically synchronize updates to a Storage backend.
type PersistentJar struct {
	http.CookieJar
	proxyURL   string
	backend    Storage
	mu         sync.Mutex
	cookiesMap map[cookieKey]Cookie
}

// Cookies returns non-expired cookies matching the target URL,
// automatically purging any expired cookies encountered during lookup.
func (pj *PersistentJar) Cookies(u *url.URL) []*http.Cookie {
	cookies := pj.CookieJar.Cookies(u)
	if len(cookies) == 0 {
		return nil
	}

	now := time.Now()
	validCookies := make([]*http.Cookie, 0, len(cookies))
	hasExpired := false

	pj.mu.Lock()
	for _, c := range cookies {
		if (!c.Expires.IsZero() && c.Expires.Before(now)) || c.MaxAge < 0 {
			key := cookieKey{domain: c.Domain, path: c.Path, name: c.Name}
			delete(pj.cookiesMap, key)

			hasExpired = true

			continue
		}

		validCookies = append(validCookies, c)
	}

	if hasExpired && pj.backend != nil {
		list := make([]Cookie, 0, len(pj.cookiesMap))
		for _, c := range pj.cookiesMap {
			list = append(list, c)
		}

		_ = pj.backend.Save(pj.proxyURL, list)
	}

	pj.mu.Unlock()

	return validCookies
}

// SetCookies stores cookies in the inner jar and flushes non-expired cookies to persistent storage.
func (pj *PersistentJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	pj.CookieJar.SetCookies(u, cookies)

	pj.mu.Lock()
	now := time.Now()
	changed := false

	for _, c := range cookies {
		rawDomain := generic.Coalesce(c.Domain, u.Hostname())
		domain := strings.ToLower(rawDomain)
		path := generic.Coalesce(c.Path, "/")

		key := cookieKey{domain: domain, path: path, name: c.Name}

		isExpired := (!c.Expires.IsZero() && c.Expires.Before(now)) || c.MaxAge < 0
		if isExpired {
			if _, exists := pj.cookiesMap[key]; exists {
				delete(pj.cookiesMap, key)

				changed = true
			}

			continue
		}

		pj.cookiesMap[key] = Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   domain,
			Path:     path,
			Expires:  c.Expires,
			HTTPOnly: c.HttpOnly,
			Secure:   c.Secure,
			MaxAge:   c.MaxAge,
		}
		changed = true
	}

	for k, c := range pj.cookiesMap {
		if (!c.Expires.IsZero() && c.Expires.Before(now)) || c.MaxAge < 0 {
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

	if changed && pj.backend != nil {
		_ = pj.backend.Save(pj.proxyURL, list)
	}
}
