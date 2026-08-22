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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	jar := cookie.NewProxyIsolatedJar()
	client := fast.NewClient(option.WithCookieJar(jar))
	defer client.CloseIdleConnections()

	targetURL, err := url.Parse("https://example.com/api")
	require.NoError(t, err)

	// SetCookies
	client.SetCookies(targetURL, []*http.Cookie{
		{Name: "session_id", Value: "abc123xyz", Path: "/"},
		{Name: "role", Value: "admin", Path: "/"},
	})

	// Cookies
	cookies := client.Cookies(targetURL)
	assert.Len(t, cookies, 2)

	// FindCookie
	cookieOpt := client.FindCookie(targetURL, "session_id")
	assert.True(t, cookieOpt.IsPresent())
	c, _ := cookieOpt.Value()
	assert.Equal(t, "abc123xyz", c.Value)

	missingOpt := client.FindCookie(targetURL, "nonexistent")
	assert.False(t, missingOpt.IsPresent())

	// GetCookieValue
	valOpt := client.GetCookieValue(targetURL, "role")
	assert.True(t, valOpt.IsPresent())
	val, _ := valOpt.Value()
	assert.Equal(t, "admin", val)

	missingValOpt := client.GetCookieValue(targetURL, "nonexistent")
	assert.False(t, missingValOpt.IsPresent())
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
