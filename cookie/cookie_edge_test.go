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

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni/cookie"
)

func TestCookie_RFC6265PathAndDomainMatching(t *testing.T) {
	t.Parallel()

	jar := cookie.NewProxyIsolatedJar()

	baseDomain, err := url.Parse("https://example.com/api/v1")
	require.NoError(t, err)

	cookies := []*http.Cookie{
		{Name: "c_root", Value: "1", Path: "/", Domain: "example.com"},
		{Name: "c_api", Value: "2", Path: "/api", Domain: "example.com"},
		{Name: "c_v1", Value: "3", Path: "/api/v1", Domain: "example.com"},
		{Name: "c_v2", Value: "4", Path: "/api/v2", Domain: "example.com"},
	}
	jar.SetCookies(baseDomain, cookies)

	tests := []struct {
		name         string
		requestURL   string
		wantCookies  []string
		avoidCookies []string
	}{
		{
			name:         "api_v1_endpoint",
			requestURL:   "https://example.com/api/v1/users",
			wantCookies:  []string{"c_root", "c_api", "c_v1"},
			avoidCookies: []string{"c_v2"},
		},
		{
			name:         "api_v2_endpoint",
			requestURL:   "https://example.com/api/v2/items",
			wantCookies:  []string{"c_root", "c_api", "c_v2"},
			avoidCookies: []string{"c_v1"},
		},
		{
			name:         "root_endpoint",
			requestURL:   "https://example.com/",
			wantCookies:  []string{"c_root"},
			avoidCookies: []string{"c_api", "c_v1", "c_v2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			u, pErr := url.Parse(tt.requestURL)
			require.NoError(t, pErr)

			matched := jar.Cookies(u)

			matchedNames := make(map[string]bool)
			for _, c := range matched {
				matchedNames[c.Name] = true
			}

			for _, want := range tt.wantCookies {
				assert.True(t, matchedNames[want])
			}

			for _, avoid := range tt.avoidCookies {
				assert.False(t, matchedNames[avoid])
			}
		})
	}
}

type inMemoryStorage struct {
	mu   sync.RWMutex
	data map[string][]cookie.Cookie
}

func (m *inMemoryStorage) Save(key string, cookies []cookie.Cookie) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data[key] = cookies

	return nil
}

func (m *inMemoryStorage) Load(key string) ([]cookie.Cookie, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.data[key], nil
}

func TestCookie_ExpirationAndMaxAge(t *testing.T) {
	t.Parallel()

	storage := &inMemoryStorage{data: make(map[string][]cookie.Cookie)}
	jar := cookie.NewProxyIsolatedJar().WithStorageBackend(storage)

	u, err := url.Parse("https://app.example.com/session")
	require.NoError(t, err)

	now := time.Now()

	t.Run("expired_cookie_purge", func(t *testing.T) {
		t.Parallel()

		expiredCookie := &http.Cookie{
			Name:    "expired_token",
			Value:   "old",
			Path:    "/",
			Expires: now.Add(-10 * time.Hour),
		}
		validCookie := &http.Cookie{
			Name:    "valid_token",
			Value:   "new",
			Path:    "/",
			Expires: now.Add(24 * time.Hour),
		}

		jar.SetCookies(u, []*http.Cookie{expiredCookie, validCookie})

		res := jar.Cookies(u)

		resNames := make(map[string]bool)
		for _, c := range res {
			resNames[c.Name] = true
		}

		assert.False(t, resNames["expired_token"])
		assert.True(t, resNames["valid_token"])
	})

	t.Run("negative_max_age_deletes_cookie", func(t *testing.T) {
		t.Parallel()

		initialCookie := &http.Cookie{
			Name:   "to_delete",
			Value:  "active",
			Path:   "/",
			MaxAge: 3600,
		}
		jar.SetCookies(u, []*http.Cookie{initialCookie})

		res := jar.Cookies(u)
		assert.NotEmpty(t, res)

		// Overwrite with negative max age to delete
		deleteCookie := &http.Cookie{
			Name:   "to_delete",
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		}
		jar.SetCookies(u, []*http.Cookie{deleteCookie})

		resAfter := jar.Cookies(u)
		for _, c := range resAfter {
			assert.NotEqual(t, "to_delete", c.Name)
		}
	})
}

func TestCookie_ProxyAndCHIPSPartitioning(t *testing.T) {
	t.Parallel()

	jar := cookie.NewProxyIsolatedJar()
	u, err := url.Parse("https://api.isolated.org/auth")
	require.NoError(t, err)

	// Proxy A context
	ctxProxyA := cookie.WithProxyAddress(t.Context(), "http://proxy-a.corp:8080")
	jarA := jar.GetJar(ctxProxyA)

	// Proxy B context
	ctxProxyB := cookie.WithProxyAddress(t.Context(), "http://proxy-b.corp:8080")
	jarB := jar.GetJar(ctxProxyB)

	// Direct context (no proxy)
	jarDirect := jar.GetJar(context.Background())

	// Set cookie specifically in Proxy A
	cookieA := &http.Cookie{Name: "session_a", Value: "token-proxy-a", Path: "/"}
	jarA.SetCookies(u, []*http.Cookie{cookieA})

	// Verify Proxy A has the cookie
	cookiesA := jarA.Cookies(u)
	require.Len(t, cookiesA, 1)
	assert.Equal(t, "session_a", cookiesA[0].Name)

	// Verify Proxy B has zero cookies (strict isolation)
	cookiesB := jarB.Cookies(u)
	assert.Empty(t, cookiesB)

	// Verify Direct connection has zero cookies
	cookiesDirect := jarDirect.Cookies(u)
	assert.Empty(t, cookiesDirect)

	// Set cookie in Direct connection
	cookieDirect := &http.Cookie{Name: "session_direct", Value: "token-direct", Path: "/"}
	jarDirect.SetCookies(u, []*http.Cookie{cookieDirect})

	// Verify Direct has its cookie, Proxy A has only its own, Proxy B is empty
	assert.Len(t, jarDirect.Cookies(u), 1)
	assert.Len(t, jarA.Cookies(u), 1)
	assert.Empty(t, jarB.Cookies(u))

	// CHIPS Partitioning test
	t.Run("chips_partition_key_isolation", func(t *testing.T) {
		t.Parallel()
		ctxPart1 := cookie.WithPartitionKey(t.Context(), "site1.com")
		ctxPart2 := cookie.WithPartitionKey(t.Context(), "site2.com")

		assert.Equal(t, "site1.com", cookie.GetPartitionKey(ctxPart1))
		assert.Equal(t, "site2.com", cookie.GetPartitionKey(ctxPart2))
	})
}
