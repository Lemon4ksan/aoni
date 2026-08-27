// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie_test

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/cookie"
)

func TestProxyIsolatedCookieJar_Basic(t *testing.T) {
	t.Parallel()

	pJar := cookie.NewProxyIsolatedJar()
	require.NotNil(t, pJar)

	u, err := url.Parse("https://example.com")
	require.NoError(t, err)

	c := &http.Cookie{Name: "session", Value: "abc"}

	// 1. Test standard http.CookieJar interface fallback (empty proxy URL)
	pJar.SetCookies(u, []*http.Cookie{c})
	cookies := pJar.Cookies(u)
	require.Len(t, cookies, 1)
	assert.Equal(t, "session", cookies[0].Name)
	assert.Equal(t, "abc", cookies[0].Value)
}

func TestProxyIsolatedCookieJar_ContextRetrieval(t *testing.T) {
	t.Parallel()

	pJar := cookie.NewProxyIsolatedJar()
	u, err := url.Parse("https://google.com")
	require.NoError(t, err)

	// Context without proxy
	jarNoProxy := pJar.GetJar(t.Context())
	assert.NotNil(t, jarNoProxy)

	// Context with proxy
	ctxWithProxy := cookie.WithProxyAddress(t.Context(), "http://proxy1.test:8080")
	assert.Equal(t, "http://proxy1.test:8080", cookie.GetProxyAddress(ctxWithProxy))

	jarWithProxy := pJar.GetJar(ctxWithProxy)
	assert.NotNil(t, jarWithProxy)

	// Verify they are different isolated jars
	c := &http.Cookie{Name: "auth", Value: "token-proxy"}
	jarWithProxy.SetCookies(u, []*http.Cookie{c})

	// Jar with proxy has the cookie
	assert.Len(t, jarWithProxy.Cookies(u), 1)
	// Default/no-proxy jar does not have the cookie
	assert.Empty(t, jarNoProxy.Cookies(u))
}

func TestProxyIsolatedCookieJar_DX_Methods(t *testing.T) {
	t.Parallel()

	pJar := cookie.NewProxyIsolatedJar()
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

	// Test HasCookies, FindCookie, GetCookieValue on default unproxied jar
	pJar.SetCookies(u, []*http.Cookie{cookie1})
	assert.True(t, pJar.HasCookies(u))

	foundCookie, found := pJar.FindCookie(u, "c1")
	require.True(t, found)
	assert.Equal(t, "v1", foundCookie.Value)

	foundOpt := pJar.FindCookieOptional(u, "c1")
	require.True(t, foundOpt.IsPresent())
	assert.Equal(t, "v1", foundOpt.MustValue().Value)

	val, ok := pJar.GetCookieValue(u, "c1")
	require.True(t, ok)
	assert.Equal(t, "v1", val)

	valOpt := pJar.GetCookieValueOptional(u, "c1")
	require.True(t, valOpt.IsPresent())
	assert.Equal(t, "v1", valOpt.MustValue())

	_, missing := pJar.FindCookie(u, "non_existent")
	assert.False(t, missing)
}

func TestProxyIsolatedCookieJar_JanitorAndPurgeExpired(t *testing.T) {
	t.Parallel()

	pJar := cookie.NewProxyIsolatedJar()
	u, err := url.Parse("https://example.com")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	pJar.StartJanitor(ctx, 10*time.Millisecond)

	// Expired cookie
	expired := &http.Cookie{
		Name:    "expired_session",
		Value:   "val",
		Expires: time.Now().Add(-1 * time.Hour),
	}

	pJar.SetCookiesForProxy("http://proxy.test:8080", u, []*http.Cookie{expired})

	// PurgeExpired should clean expired cookies
	pJar.PurgeExpired()
	cookies := pJar.CookiesForProxy("http://proxy.test:8080", u)
	assert.Empty(t, cookies)
}

func TestProxyIsolatedCookieJar_ConcurrentUsage(t *testing.T) {
	t.Parallel()

	pJar := cookie.NewProxyIsolatedJar()

	var wg sync.WaitGroup

	proxies := []string{
		"http://p1.com", "http://p2.com", "http://p3.com", "http://p4.com",
		"http://p1.com", "http://p2.com", "http://p3.com", "http://p4.com",
	}

	for _, p := range proxies {
		wg.Add(1)

		go func(proxy string) {
			defer wg.Done()

			jar := pJar.GetJarForProxy(proxy)
			assert.NotNil(t, jar)
		}(p)
	}

	wg.Wait()
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestCookieTransport_RoundTrip_And_PartitionKey(t *testing.T) {
	t.Parallel()

	// 1. Partition Key
	ctxPart := cookie.WithPartitionKey(t.Context(), "https://partition.example.com")
	assert.Equal(t, "https://partition.example.com", cookie.GetPartitionKey(ctxPart))

	// 2. Cookie Transport
	pJar := cookie.NewProxyIsolatedJar()
	u, _ := url.Parse("https://example.com/api")
	pJar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "active-123"}})

	mockRT := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "session=active-123", req.Header.Get("Cookie"))

		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Set-Cookie": []string{"tracker=xyz; Path=/"},
			},
		}

		return resp, nil
	})

	tr := &cookie.Transport{
		Next:      mockRT,
		CookieJar: pJar,
	}

	assert.NotNil(t, tr.Unwrap())
	cloned := tr.CloneTransport(http.DefaultTransport)
	assert.NotNil(t, cloned)

	req, err := http.NewRequestWithContext(t.Context(), "GET", "https://example.com/api", nil)
	require.NoError(t, err)

	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify tracker cookie captured
	cookies := pJar.Cookies(u)
	assert.NotEmpty(t, cookies)
}

