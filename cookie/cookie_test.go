// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cookie_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/cookie"
)

func TestMirrorCookies(t *testing.T) {
	t.Parallel()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)

	srcURL, _ := url.Parse("https://example.com")
	target1, _ := url.Parse("https://sub.example.com")
	target2, _ := url.Parse("https://other-domain.com")

	// Empty jar case (returns early)
	cookie.Mirror(jar, srcURL, []*url.URL{target1}, "session")
	assert.Empty(t, jar.Cookies(target1))

	// Set source cookies
	jar.SetCookies(srcURL, []*http.Cookie{
		{Name: "session", Value: "abc"},
		{Name: "tracker", Value: "xyz"},
	})

	// Mirror unmatched cookies (returns early)
	cookie.Mirror(jar, srcURL, []*url.URL{target1}, "non-existent")
	assert.Empty(t, jar.Cookies(target1))

	// Mirror matched cookies successfully
	cookie.Mirror(jar, srcURL, []*url.URL{target1, target2}, "session")

	// Verify only "session" is mirrored, "tracker" is not
	cookies1 := jar.Cookies(target1)
	require.Len(t, cookies1, 1)
	assert.Equal(t, "session", cookies1[0].Name)
	assert.Equal(t, "abc", cookies1[0].Value)

	cookies2 := jar.Cookies(target2)
	require.Len(t, cookies2, 1)
	assert.Equal(t, "session", cookies2[0].Name)
}

func TestFilterForRequest(t *testing.T) {
	t.Parallel()

	c1 := &http.Cookie{Name: "c1", Path: "/api/v1"}
	c2 := &http.Cookie{Name: "c2", Path: "/admin"}

	u, _ := url.Parse("https://example.com/api/v1/users")
	filtered := cookie.FilterForRequest([]*http.Cookie{c1, c2}, u)

	require.Len(t, filtered, 1)
	assert.Equal(t, "c1", filtered[0].Name)
}

func TestBuildCookieHeader_And_SortForBrowser(t *testing.T) {
	t.Parallel()

	c1 := &http.Cookie{Name: "short", Value: "val1", Path: "/api"}
	c2 := &http.Cookie{Name: "long", Value: "val2", Path: "/api/v1/users"}
	c3 := &http.Cookie{Name: "root", Value: "val3", Path: "/"}

	cookies := []*http.Cookie{c1, c2, c3}
	cookie.SortForBrowser(cookies)

	assert.Equal(t, "/api/v1/users", cookies[0].Path)
	assert.Equal(t, "/api", cookies[1].Path)
	assert.Equal(t, "/", cookies[2].Path)

	hdr := cookie.BuildCookieHeader(cookies)
	assert.Equal(t, "long=val2; short=val1; root=val3", hdr)
}

func TestCookieExportAndImport_Slice(t *testing.T) {
	t.Parallel()

	jar1, _ := cookiejar.New(nil)
	u, _ := url.Parse("https://example.com")

	// Nil / Empty boundaries
	assert.Nil(t, cookie.Export(nil, u))
	assert.Nil(t, cookie.Export(jar1, nil))
	cookie.Import(nil, u, nil)
	cookie.Import(jar1, nil, nil)

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

	exported := cookie.Export(jar1, u)
	require.Len(t, exported, 1)
	assert.Equal(t, "session", exported[0].Name)
	assert.Equal(t, "token123", exported[0].Value)

	// Import to fresh jar
	jar2, _ := cookiejar.New(nil)
	cookie.Import(jar2, u, exported)

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
	strEmpty, err := cookie.ExportJSON(jar1, u)
	require.NoError(t, err)
	assert.Equal(t, "[]", strEmpty)

	err = cookie.ImportJSON(nil, nil, "")
	require.NoError(t, err)

	jar1.SetCookies(u, []*http.Cookie{
		{Name: "session", Value: "123"},
	})

	// Export to JSON string
	jsonStr, err := cookie.ExportJSON(jar1, u)
	require.NoError(t, err)
	assert.Contains(t, jsonStr, `"name":"session"`)
	assert.Contains(t, jsonStr, `"value":"123"`)

	// Import from JSON string to fresh jar
	jar2, _ := cookiejar.New(nil)
	err = cookie.ImportJSON(jar2, u, jsonStr)
	require.NoError(t, err)

	cookies := jar2.Cookies(u)
	require.Len(t, cookies, 1)
	assert.Equal(t, "session", cookies[0].Name)
	assert.Equal(t, "123", cookies[0].Value)

	// Import invalid JSON error handling
	err = cookie.ImportJSON(jar2, u, "{invalid-json")
	assert.Error(t, err)
}

func TestExportNetscape(t *testing.T) {
	t.Parallel()

	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse("https://example.com/api")

	jar.SetCookies(u, []*http.Cookie{
		{Name: "session", Value: "abc123", Domain: ".example.com", Path: "/api", Secure: true},
	})

	netscapeText := cookie.ExportNetscape(jar, u)
	assert.Contains(t, netscapeText, "# Netscape HTTP Cookie File")
	assert.Contains(t, netscapeText, "example.com\tFALSE\t/\tFALSE\t0\tsession\tabc123")
}

func TestProxyIsolatedJar_FindCookie_And_GetCookieValue(t *testing.T) {
	t.Parallel()

	jar := cookie.NewProxyIsolatedJar()
	u, _ := url.Parse("https://example.com/login")

	jar.SetCookies(u, []*http.Cookie{
		{Name: "auth_token", Value: "secret-token-123"},
		{Name: "theme", Value: "dark"},
	})

	// FindCookie
	cOpt := jar.FindCookie(u, "auth_token")
	require.True(t, cOpt.IsPresent())
	c, ok := cOpt.Value()
	require.True(t, ok)
	assert.Equal(t, "secret-token-123", c.Value)

	// Missing cookie
	missingOpt := jar.FindCookie(u, "non_existent")
	assert.False(t, missingOpt.IsPresent())

	// GetCookieValue
	valOpt := jar.GetCookieValue(u, "theme")
	require.True(t, valOpt.IsPresent())
	assert.Equal(t, "dark", valOpt.ValueOr("light"))

	missingValOpt := jar.GetCookieValue(u, "missing_setting")
	assert.Equal(t, "default_setting", missingValOpt.ValueOr("default_setting"))
}
