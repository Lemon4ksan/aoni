// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

func TestFastClient_HTTPVerbParity(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Echo-Method", r.Method)
		w.Header().Set("X-Echo-Path", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer ts.Close()

	client := fast.NewClient(option.WithBaseURL(ts.URL))
	defer client.CloseIdleConnections()

	ctx := context.Background()

	// 1. GET
	resp, err := client.Get(ctx, "/get-endpoint")
	require.NoError(t, err)
	assert.Equal(t, "GET", resp.Header("X-Echo-Method"))
	assert.Equal(t, "/get-endpoint", resp.Header("X-Echo-Path"))
	resp.Close()

	// 2. POST
	resp, err = client.Post(ctx, "/post-endpoint", mod.WithJSON(map[string]string{"foo": "bar"}))
	require.NoError(t, err)
	assert.Equal(t, "POST", resp.Header("X-Echo-Method"))
	assert.JSONEq(t, `{"foo":"bar"}`, string(resp.BodyBytes()))
	resp.Close()

	// 3. PUT
	resp, err = client.Put(ctx, "/put-endpoint", mod.WithSmartBody("hello-put"))
	require.NoError(t, err)
	assert.Equal(t, "PUT", resp.Header("X-Echo-Method"))
	assert.Equal(t, "hello-put", string(resp.BodyBytes()))
	resp.Close()

	// 4. PATCH
	resp, err = client.Patch(ctx, "/patch-endpoint", mod.WithSmartBody("hello-patch"))
	require.NoError(t, err)
	assert.Equal(t, "PATCH", resp.Header("X-Echo-Method"))
	assert.Equal(t, "hello-patch", string(resp.BodyBytes()))
	resp.Close()

	// 5. DELETE
	resp, err = client.Delete(ctx, "/delete-endpoint")
	require.NoError(t, err)
	assert.Equal(t, "DELETE", resp.Header("X-Echo-Method"))
	resp.Close()

	// 6. HEAD
	resp, err = client.Head(ctx, "/head-endpoint")
	require.NoError(t, err)
	assert.Equal(t, "HEAD", resp.Header("X-Echo-Method"))
	resp.Close()

	// 7. OPTIONS
	resp, err = client.Options(ctx, "/options-endpoint")
	require.NoError(t, err)
	assert.Equal(t, "OPTIONS", resp.Header("X-Echo-Method"))
	resp.Close()

	// 8. DoBaremetal
	resp, err = client.DoBaremetal(ctx, http.MethodGet, "/baremetal-endpoint")
	require.NoError(t, err)
	assert.Equal(t, "GET", resp.Header("X-Echo-Method"))
	assert.Equal(t, "/baremetal-endpoint", resp.Header("X-Echo-Path"))
	resp.Close()
}

func TestFastClient_CookieParity(t *testing.T) {
	t.Parallel()

	targetURL, err := url.Parse("https://example.com/api")
	require.NoError(t, err)

	testCookies := []*http.Cookie{
		{Name: "session_id", Value: "abc123xyz", Path: "/"},
		{Name: "role", Value: "admin", Path: "/"},
	}

	t.Run("fast.Client", func(t *testing.T) {
		t.Parallel()

		client := fast.NewClient(option.WithCookieJar(cookie.NewProxyIsolatedJar()))
		defer client.CloseIdleConnections()

		assert.False(t, client.HasCookies(targetURL))

		client.SetCookies(targetURL, testCookies)
		assert.True(t, client.HasCookies(targetURL))
		assert.Len(t, client.Cookies(targetURL), 2)

		// FindCookie
		c, ok := client.FindCookie(targetURL, "session_id")
		require.True(t, ok)
		assert.Equal(t, "abc123xyz", c.Value)

		cOpt := client.FindCookieOptional(targetURL, "session_id")
		require.True(t, cOpt.IsPresent())
		assert.Equal(t, "abc123xyz", cOpt.MustValue().Value)

		_, missing := client.FindCookie(targetURL, "nonexistent")
		assert.False(t, missing)

		// GetCookieValue
		val, okVal := client.GetCookieValue(targetURL, "role")
		require.True(t, okVal)
		assert.Equal(t, "admin", val)

		valOpt := client.GetCookieValueOptional(targetURL, "role")
		require.True(t, valOpt.IsPresent())
		assert.Equal(t, "admin", valOpt.ValueOr("guest"))
	})

	t.Run("aoni.Client", func(t *testing.T) {
		t.Parallel()

		client := aoni.NewClient(option.WithCookieJar(cookie.NewProxyIsolatedJar()))
		defer client.CloseIdleConnections()

		assert.False(t, client.HasCookies(targetURL))

		client.SetCookies(targetURL, testCookies)
		assert.True(t, client.HasCookies(targetURL))
		assert.Len(t, client.Cookies(targetURL), 2)

		// FindCookie
		c, ok := client.FindCookie(targetURL, "session_id")
		require.True(t, ok)
		assert.Equal(t, "abc123xyz", c.Value)

		cOpt := client.FindCookieOptional(targetURL, "session_id")
		require.True(t, cOpt.IsPresent())
		assert.Equal(t, "abc123xyz", cOpt.MustValue().Value)

		_, missing := client.FindCookie(targetURL, "nonexistent")
		assert.False(t, missing)

		// GetCookieValue
		val, okVal := client.GetCookieValue(targetURL, "role")
		require.True(t, okVal)
		assert.Equal(t, "admin", val)

		valOpt := client.GetCookieValueOptional(targetURL, "role")
		require.True(t, valOpt.IsPresent())
		assert.Equal(t, "admin", valOpt.ValueOr("guest"))
	})
}

func TestFastClient_TelemetryParity(t *testing.T) {
	t.Parallel()

	client := fast.NewClient(
		option.WithBaseURL("https://api.example.com"),
		option.WithChrome(),
	)
	defer client.CloseIdleConnections()

	logVal := client.LogValue()
	assert.Equal(t, "fasthttp", logVal.Group()[0].Value.String())
	assert.Equal(t, "https://api.example.com/", logVal.Group()[1].Value.String())
	assert.Equal(t, aoni.BrowserChrome.String(), logVal.Group()[2].Value.String())
}
