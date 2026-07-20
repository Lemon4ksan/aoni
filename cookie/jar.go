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

	"github.com/lemon4ksan/miyako/sync/keylock"
)

type proxyCtxKey struct{}

// WithProxyAddress attaches the active proxy URL string to the context.
func WithProxyAddress(ctx context.Context, addr string) context.Context {
	return context.WithValue(ctx, proxyCtxKey{}, addr)
}

// GetProxyAddress extracts the active proxy URL string from the context.
func GetProxyAddress(ctx context.Context) string {
	if val := ctx.Value(proxyCtxKey{}); val != nil {
		return val.(string)
	}

	return ""
}

// ProxyIsolatedJar is an isolated cookie jar that stores cookies per proxy URL.
// It is safe for concurrent use by multiple goroutines.
//
// Cookie isolation works for direct requests: the proxy URL is extracted from the
// request context and used to select the correct per-proxy jar.
//
// # Concurrency and Thread Safety
//
// It uses a combination of an internal read-write mutex protecting
// the active jars map, and a generic [keylock.KeyMutex] to synchronize the
// initialization of individual per-proxy jars. This ensures that:
//   - Reading or accessing an already initialized proxy-specific jar is a non-blocking,
//     lock-free read path for concurrent requests.
//   - If multiple parallel goroutines trigger the initial setup or persistent loading
//     of a jar for the exact same proxy, the keylock serializes the initialization,
//     completely preventing duplicate database/storage load operations and map races.
//
// # Limitations
//
// During HTTP redirects, the standard library's http.Client calls
// SetCookies/Cookies without passing the request context. In this case the jar
// falls back to the default (empty-key) jar. This means per-proxy cookie isolation
// does not apply to redirect responses. In practice this is rarely an issue because
// proxy servers typically do not return redirects, and cookies from the target server
// arrive in the initial response before any redirect.
type ProxyIsolatedJar struct {
	mu      sync.RWMutex
	jars    map[string]http.CookieJar
	km      keylock.KeyMutex[string]
	backend Storage
}

// NewProxyIsolatedJar creates a new ProxyIsolatedCookieJar.
func NewProxyIsolatedJar() *ProxyIsolatedJar {
	return &ProxyIsolatedJar{
		jars: make(map[string]http.CookieJar),
	}
}

// SetCookies implements the http.CookieJar interface.
// For direct requests, uses the proxy URL from the request context.
// For redirects (no context available), falls back to the default jar.
func (p *ProxyIsolatedJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	jar := p.GetJarForProxy("")
	if jar != nil {
		jar.SetCookies(u, cookies)
	}
}

// Cookies implements the http.CookieJar interface.
// For direct requests, uses the proxy URL from the request context.
// For redirects (no context available), falls back to the default jar.
func (p *ProxyIsolatedJar) Cookies(u *url.URL) []*http.Cookie {
	jar := p.GetJarForProxy("")
	if jar != nil {
		return jar.Cookies(u)
	}

	return nil
}

// GetJarForProxy returns the specific [http.CookieJar] associated with the given proxy URL.
// This is a high-level helper to manage proxy cookies programmatically.
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
	p.mu.RUnlock()

	if ok {
		return jar
	}

	baseJar, err := cookiejar.New(nil)
	if err != nil {
		return nil
	}

	p.mu.RLock()
	backend := p.backend
	p.mu.RUnlock()

	if backend != nil {
		pJar := &PersistentJar{
			CookieJar:  baseJar,
			proxyURL:   proxyURL,
			backend:    backend,
			cookiesMap: make(map[string]Cookie),
		}
		// Load existing cookies from backend
		if cookies, err := backend.Load(proxyURL); err == nil && len(cookies) > 0 {
			pJar.mu.Lock()
			for _, c := range cookies {
				key := c.Domain + "|" + c.Path + "|" + c.Name
				pJar.cookiesMap[key] = c

				scheme := "http"
				if c.Secure {
					scheme = "https"
				}

				domain := strings.TrimPrefix(c.Domain, ".")

				u, parseErr := url.Parse(scheme + "://" + domain + c.Path)
				if parseErr == nil {
					baseJar.SetCookies(u, []*http.Cookie{
						{ // nolint:gosec
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

			pJar.mu.Unlock()
		}

		jar = pJar
	} else {
		jar = baseJar
	}

	p.mu.Lock()
	p.jars[proxyURL] = jar
	p.mu.Unlock()

	return jar
}

// WithStorageBackend configures a persistent storage backend for cookies.
func (p *ProxyIsolatedJar) WithStorageBackend(backend Storage) *ProxyIsolatedJar {
	p.mu.Lock()
	p.backend = backend
	p.mu.Unlock()

	return p
}

// CookiesForProxy manually retrieves cookies for a specific proxy URL.
func (p *ProxyIsolatedJar) CookiesForProxy(proxyURL string, u *url.URL) []*http.Cookie {
	jar := p.GetJarForProxy(proxyURL)
	if jar != nil {
		return jar.Cookies(u)
	}

	return nil
}

// GetJar returns the cookie jar for the given context, creating it if necessary.
func (p *ProxyIsolatedJar) GetJar(ctx context.Context) http.CookieJar {
	proxyURL := GetProxyAddress(ctx) // Используем наш хелпер
	return p.GetJarForProxy(proxyURL)
}

// PersistentJar wraps http.CookieJar to save state to a CookieStorageBackend.
type PersistentJar struct {
	http.CookieJar
	proxyURL   string
	backend    Storage
	mu         sync.Mutex
	cookiesMap map[string]Cookie
}

// SetCookies implements the http.CookieJar interface.
func (pj *PersistentJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	pj.CookieJar.SetCookies(u, cookies)

	pj.mu.Lock()
	changed := false
	now := time.Now()

	for _, c := range cookies {
		// If cookie is expired or max-age <= 0, remove it
		if !c.Expires.IsZero() && c.Expires.Before(now) {
			key := c.Domain + "|" + c.Path + "|" + c.Name
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

		key := domain + "|" + path + "|" + c.Name
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

	// Filter out expired cookies from cookiesMap
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

// SetCookiesForProxy manually stores cookies for a specific proxy URL.
func (p *ProxyIsolatedJar) SetCookiesForProxy(proxyURL string, u *url.URL, cookies []*http.Cookie) {
	jar := p.GetJarForProxy(proxyURL)
	if jar != nil {
		jar.SetCookies(u, cookies)
	}
}
