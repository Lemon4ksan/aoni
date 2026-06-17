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
)

type proxyCtxKey struct{}

// ProxyIsolatedCookieJar is an isolated cookie jar that stores cookies per proxy URL.
type ProxyIsolatedCookieJar struct {
	mu   sync.RWMutex
	jars map[string]http.CookieJar
}

// NewProxyIsolatedCookieJar creates a new ProxyIsolatedCookieJar.
func NewProxyIsolatedCookieJar() *ProxyIsolatedCookieJar {
	return &ProxyIsolatedCookieJar{
		jars: make(map[string]http.CookieJar),
	}
}

// SetCookies implements the http.CookieJar interface (as a fallback for standard calls).
func (p *ProxyIsolatedCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	jar := p.getJar(context.Background())
	if jar != nil {
		jar.SetCookies(u, cookies)
	}
}

// Cookies implements the http.CookieJar interface (as a fallback for standard calls).
func (p *ProxyIsolatedCookieJar) Cookies(u *url.URL) []*http.Cookie {
	jar := p.getJar(context.Background())
	if jar != nil {
		return jar.Cookies(u)
	}

	return nil
}

// getJar returns the cookie jar for the given context, creating it if necessary.
func (p *ProxyIsolatedCookieJar) getJar(ctx context.Context) http.CookieJar {
	p.mu.Lock()
	defer p.mu.Unlock()

	proxyURL := ""
	if val := ctx.Value(proxyCtxKey{}); val != nil {
		proxyURL = val.(string)
	}

	jar, ok := p.jars[proxyURL]
	if !ok {
		var err error

		jar, err = cookiejar.New(nil)
		if err != nil {
			return nil
		}

		p.jars[proxyURL] = jar
	}

	return jar
}
