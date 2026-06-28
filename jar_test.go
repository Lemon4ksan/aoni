// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyIsolatedCookieJar_Basic(t *testing.T) {
	t.Parallel()

	pJar := NewProxyIsolatedCookieJar()
	require.NotNil(t, pJar)

	u, err := url.Parse("https://example.com")
	require.NoError(t, err)

	cookie := &http.Cookie{Name: "session", Value: "abc"}

	// 1. Test standard http.CookieJar interface fallback (empty proxy URL)
	pJar.SetCookies(u, []*http.Cookie{cookie})
	cookies := pJar.Cookies(u)
	require.Len(t, cookies, 1)
	assert.Equal(t, "session", cookies[0].Name)
	assert.Equal(t, "abc", cookies[0].Value)
}

func TestProxyIsolatedCookieJar_ContextRetrieval(t *testing.T) {
	t.Parallel()

	pJar := NewProxyIsolatedCookieJar()
	u, err := url.Parse("https://google.com")
	require.NoError(t, err)

	// Context without proxy
	jarNoProxy := pJar.GetJar(t.Context())
	assert.NotNil(t, jarNoProxy)

	// Context with proxy
	ctxWithProxy := context.WithValue(t.Context(), proxyCtxKey{}, "http://proxy1.test:8080")
	jarWithProxy := pJar.GetJar(ctxWithProxy)
	assert.NotNil(t, jarWithProxy)

	// Verify they are different isolated jars
	cookie := &http.Cookie{Name: "auth", Value: "token-proxy"}
	jarWithProxy.SetCookies(u, []*http.Cookie{cookie})

	// Jar with proxy has the cookie
	assert.Len(t, jarWithProxy.Cookies(u), 1)
	// Default/no-proxy jar does not have the cookie
	assert.Empty(t, jarNoProxy.Cookies(u))
}

func TestProxyIsolatedCookieJar_DX_Methods(t *testing.T) {
	t.Parallel()

	pJar := NewProxyIsolatedCookieJar()
	u, err := url.Parse("https://yahoo.com")
	require.NoError(t, err)

	cookie1 := &http.Cookie{Name: "c1", Value: "v1"}
	cookie2 := &http.Cookie{Name: "c2", Value: "v2"}

	// Explicitly set cookies for proxy1
	pJar.SetCookiesForProxy("http://proxy1.net", u, []*http.Cookie{cookie1})
	// Explicitly set cookies for proxy2
	pJar.SetCookiesForProxy("http://proxy2.net", u, []*http.Cookie{cookie2})

	// Read back and assert isolation
	cProxy1 := pJar.CookiesForProxy("http://proxy1.net", u)
	require.Len(t, cProxy1, 1)
	assert.Equal(t, "v1", cProxy1[0].Value)

	cProxy2 := pJar.CookiesForProxy("http://proxy2.net", u)
	require.Len(t, cProxy2, 1)
	assert.Equal(t, "v2", cProxy2[0].Value)

	// Retrieve the underlying jar directly
	jar1 := pJar.GetJarForProxy("http://proxy1.net")
	assert.NotNil(t, jar1)
	assert.Equal(t, cProxy1, jar1.Cookies(u))
}

func TestProxyIsolatedCookieJar_ConcurrentUsage(t *testing.T) {
	t.Parallel()

	pJar := NewProxyIsolatedCookieJar()

	var wg sync.WaitGroup

	proxies := []string{
		"http://p1.com", "http://p2.com", "http://p3.com", "http://p4.com",
		"http://p1.com", "http://p2.com", "http://p3.com", "http://p4.com", // triggers RLock hits
	}

	for _, proxy := range proxies {
		wg.Add(1)

		go func(p string) {
			defer wg.Done()

			jar := pJar.GetJarForProxy(p)
			assert.NotNil(t, jar)
		}(proxy)
	}

	wg.Wait()
}
