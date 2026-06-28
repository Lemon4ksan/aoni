// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyAwareSessionCache_Operations(t *testing.T) {
	t.Parallel()

	cache := NewProxyAwareSessionCache()
	require.NotNil(t, cache)

	// Set initial proxy key
	cache.SetProxyKey("http://proxy1.net")
	assert.Equal(t, "http://proxy1.net", cache.CurrentProxyKey())

	// Put dummy state (nil state is handled by standard LRU cache safely)
	cache.Put("google.com", nil)

	// Retrieve (should be present, even if nil)
	_, ok := cache.Get("google.com")
	assert.True(t, ok)

	// Change proxy key (must invalidate/clear the cache)
	cache.SetProxyKey("http://proxy2.net")
	assert.Equal(t, "http://proxy2.net", cache.CurrentProxyKey())

	_, ok = cache.Get("google.com")
	assert.False(t, ok, "cache should be cleared after proxy key change")

	// Verify same proxy key does not invalidate
	cache.Put("yahoo.com", nil)
	cache.SetProxyKey("http://proxy2.net") // same key
	_, ok = cache.Get("yahoo.com")
	assert.True(t, ok)

	// Manual Clear
	cache.Clear()
	_, ok = cache.Get("yahoo.com")
	assert.False(t, ok)
}

func TestMirrorCookies(t *testing.T) {
	t.Parallel()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)

	srcURL, _ := url.Parse("https://example.com")
	target1, _ := url.Parse("https://sub.example.com")
	target2, _ := url.Parse("https://other-domain.com")

	// 1. Empty jar case (returns early)
	MirrorCookies(jar, srcURL, []*url.URL{target1}, "session")
	assert.Empty(t, jar.Cookies(target1))

	// Set source cookies
	jar.SetCookies(srcURL, []*http.Cookie{
		{Name: "session", Value: "abc"},
		{Name: "tracker", Value: "xyz"},
	})

	// 2. Mirror unmatched cookies (returns early)
	MirrorCookies(jar, srcURL, []*url.URL{target1}, "non-existent")
	assert.Empty(t, jar.Cookies(target1))

	// 3. Mirror matched cookies successfully
	MirrorCookies(jar, srcURL, []*url.URL{target1, target2}, "session")

	// Verify only "session" is mirrored, "tracker" is not
	cookies1 := jar.Cookies(target1)
	require.Len(t, cookies1, 1)
	assert.Equal(t, "session", cookies1[0].Name)
	assert.Equal(t, "abc", cookies1[0].Value)

	cookies2 := jar.Cookies(target2)
	require.Len(t, cookies2, 1)
	assert.Equal(t, "session", cookies2[0].Name)
}

func TestCookieExportAndImport_Slice(t *testing.T) {
	t.Parallel()

	jar1, _ := cookiejar.New(nil)
	u, _ := url.Parse("https://example.com")

	// Nil / Empty boundaries
	assert.Nil(t, ExportCookies(nil, u))
	assert.Nil(t, ExportCookies(jar1, nil))
	ImportCookies(nil, u, nil)
	ImportCookies(jar1, nil, nil)

	// Populating cookies
	expires := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	jar1.SetCookies(u, []*http.Cookie{
		{
			Name:     "session",
			Value:    "token123",
			Domain:   "example.com",
			Path:     "/",
			Expires:  expires,
			HttpOnly: true,
			Secure:   true,
		},
	})

	// Note: Go's standard net/http/cookiejar.Cookies(u) only returns Name and Value fields,
	// leaving other attributes (Secure, HttpOnly, Domain, Path, etc.) at their zero values
	// because they are not used when sending cookies inside HTTP Request headers.
	exported := ExportCookies(jar1, u)
	require.Len(t, exported, 1)
	assert.Equal(t, "session", exported[0].Name)
	assert.Equal(t, "token123", exported[0].Value)

	// Import to fresh jar
	jar2, _ := cookiejar.New(nil)
	ImportCookies(jar2, u, exported)

	imported := jar2.Cookies(u)
	require.Len(t, imported, 1)
	assert.Equal(t, "session", imported[0].Name)
	assert.Equal(t, "token123", imported[0].Value)
}

func TestCookieExportAndImport_JSON(t *testing.T) {
	t.Parallel()

	jar1, _ := cookiejar.New(nil)
	u, _ := url.Parse("https://example.com")

	// Boundary cases
	strEmpty, err := ExportCookiesJSON(jar1, u)
	require.NoError(t, err)
	assert.Equal(t, "[]", strEmpty)

	err = ImportCookiesJSON(nil, nil, "")
	require.NoError(t, err)

	jar1.SetCookies(u, []*http.Cookie{
		{Name: "session", Value: "123"},
	})

	// Export to JSON string
	jsonStr, err := ExportCookiesJSON(jar1, u)
	require.NoError(t, err)
	assert.Contains(t, jsonStr, `"name":"session"`)
	assert.Contains(t, jsonStr, `"value":"123"`)

	// Import from JSON string to fresh jar
	jar2, _ := cookiejar.New(nil)
	err = ImportCookiesJSON(jar2, u, jsonStr)
	require.NoError(t, err)

	cookies := jar2.Cookies(u)
	require.Len(t, cookies, 1)
	assert.Equal(t, "session", cookies[0].Name)
	assert.Equal(t, "123", cookies[0].Value)

	// Import invalid JSON error handling
	err = ImportCookiesJSON(jar2, u, "{invalid-json")
	assert.Error(t, err)
}
