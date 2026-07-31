// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
)

// TestCookie_ParsingAndAttributes tests parsing and transmission of Cookie attributes via bridge.
func TestCookie_ParsingAndAttributes(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie1 := &http.Cookie{
			Name:     "auth_token",
			Value:    "jwt-value-123",
			Path:     "/api",
			Domain:   "127.0.0.1",
			Expires:  time.Now().Add(24 * time.Hour),
			HttpOnly: true,
			Secure:   false,
			SameSite: http.SameSiteLaxMode,
		}
		cookie2 := &http.Cookie{
			Name:  "theme",
			Value: "dark",
		}

		http.SetCookie(w, cookie1)
		http.SetCookie(w, cookie2)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := fast.NewClient()
	stdClient := fast.NewStdClient(c)
	stdResp, err := stdClient.Get(ts.URL + "/api/test")
	require.NoError(t, err)

	defer stdResp.Body.Close()

	cookies := stdResp.Cookies()
	require.Len(t, cookies, 2)

	var authCookie, themeCookie *http.Cookie
	for _, ck := range cookies {
		switch ck.Name {
		case "auth_token":
			authCookie = ck
		case "theme":
			themeCookie = ck
		}
	}

	require.NotNil(t, authCookie)
	assert.Equal(t, "jwt-value-123", authCookie.Value)
	assert.Equal(t, "/api", authCookie.Path)
	assert.True(t, authCookie.HttpOnly)

	require.NotNil(t, themeCookie)
	assert.Equal(t, "dark", themeCookie.Value)
}

// TestHeader_MultiValueAndCaseInsensitivity tests case-insensitive header access and multiple header values.
func TestHeader_MultiValueAndCaseInsensitivity(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Custom-Header", "value1")
		w.Header().Add("X-Custom-Header", "value2")
		w.Header().Add("x-custom-header", "value3")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := fast.NewClient()
	resp, err := c.Request(context.Background(), "GET", ts.URL)
	require.NoError(t, err)

	defer resp.Close()

	// Case-insensitive check
	assert.NotEmpty(t, resp.Header("x-custom-header"))
	assert.NotEmpty(t, resp.Header("X-CUSTOM-HEADER"))

	// All header map values
	headers := resp.Headers()
	vals := headers["X-Custom-Header"]
	assert.True(t, len(vals) >= 1)
}

// TestHeader_RequestModifiers verifies applying headers via functional request modifiers.
func TestHeader_RequestModifiers(t *testing.T) {
	var receivedHeaders http.Header

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := fast.NewClient()
	resp, err := c.Request(context.Background(), "GET", ts.URL,
		mod.WithHeader("X-App-Version", "2.5.0"),
		mod.WithHeader("Accept-Language", "en-US,en;q=0.9"),
		mod.WithBearer("my-access-token"),
	)
	require.NoError(t, err)
	resp.Close()

	assert.Equal(t, "2.5.0", receivedHeaders.Get("X-App-Version"))
	assert.Equal(t, "en-US,en;q=0.9", receivedHeaders.Get("Accept-Language"))
	assert.Equal(t, "Bearer my-access-token", receivedHeaders.Get("Authorization"))
}
