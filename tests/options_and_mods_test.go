// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

func TestOptions_Coverage(t *testing.T) {
	t.Parallel()

	cfg := &aoni.Config{Defaults: aoni.ClientDefaults{Headers: make(http.Header)}}
	option.WithTimeout(5 * time.Second)(cfg)
	option.WithUserAgent("CustomUA")(cfg)
	option.WithBaseURL("https://example.com")(cfg)
	option.WithHeaders(map[string]string{"X-Header": "Val"})(cfg)

	assert.Equal(t, 5*time.Second, cfg.Engine.Timeout)
	assert.Equal(t, "CustomUA", cfg.Defaults.Headers.Get("User-Agent"))
	assert.Equal(t, "https://example.com/", cfg.Defaults.BaseURL.String())
	assert.Equal(t, "Val", cfg.Defaults.Headers.Get("X-Header"))
}

func TestOption_WithDefaultHeaders(t *testing.T) {
	t.Parallel()

	cfg := &aoni.Config{Defaults: aoni.ClientDefaults{Headers: make(http.Header)}}
	option.WithHeaders(map[string]string{
		"User-Agent": "CustomUA",
		"Origin":     "https://origin.local",
	})(cfg)

	assert.Equal(t, "CustomUA", cfg.Defaults.Headers.Get("User-Agent"))
	assert.Equal(t, "https://origin.local", cfg.Defaults.Headers.Get("Origin"))
}

func TestModifiers_Coverage(t *testing.T) {
	t.Parallel()

	httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.com/users/{id}", nil)
	require.NoError(t, err)

	req := aoni.NewStdRequest(httpReq)
	mod.WithVar("id", 123)(req)
	assert.Equal(t, "/users/123", req.Path())

	httpReq2, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.com/item/{cat}/{id}", nil)
	req2 := aoni.NewStdRequest(httpReq2)
	mod.WithVars("cat", "books", "id", 42)(req2)
	assert.Equal(t, "/item/books/42", req2.Path())

	httpReq3, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
	req3 := aoni.NewStdRequest(httpReq3)
	mod.WithHeaders(map[string]string{"X-A": "1", "X-B": "2"})(req3)
	assert.Equal(t, "1", req3.Header("X-A"))

	mod.ResetHeaders()(req3)
	assert.Empty(t, req3.Header("X-A"))

	mod.WithBearer("secret")(req3)
	assert.Equal(t, "Bearer secret", req3.Header("Authorization"))

	mod.WithBasicAuth("admin", "pass")(req3)
	assert.Contains(t, req3.Header("Authorization"), "Basic")

	mod.WithUserAgent("AgentX")(req3)
	assert.Equal(t, "AgentX", req3.Header("User-Agent"))

	mod.WithCookie(&http.Cookie{Name: "c1", Value: "v1"})(req3)
	mod.WithCookies(map[string]string{"c2": "v2"})(req3)

	mod.WithJSONBody(map[string]string{"foo": "bar"})(req3)
	assert.Equal(t, "application/json", req3.Header("Content-Type"))

	mod.WithOrigin("https://test.com")(req3)
	assert.Equal(t, "https://test.com", req3.Header("Origin"))

	type formPayload struct {
		Field string `url:"field"`
	}
	mod.WithFormBody(formPayload{Field: "value"})(req3)
	assert.Equal(t, "application/x-www-form-urlencoded", req3.Header("Content-Type"))

	mod.WithOrderedHeaders([]string{"user-agent", "host"})(req3)
	mod.WithALPN("h2", "http/1.1")(req3)
	mod.WithMultiReadThreshold(1024)(req3)
	mod.WithMultiReadDisableDisk(true)(req3)
	mod.WithSSRFGuard()(req3)
	mod.WithHappyEyeballs(10 * time.Millisecond)(req3)
	mod.WithProxyDNS()(req3)
	mod.WithP0fSignature(p0f.Linux3x)(req3)
	mod.WithForceHTTP1()(req3)
	mod.WithForceHTTP2()(req3)
	mod.WithForceHTTP3()(req3)

	cfg := aoni.GetRequestConfig(req3.Context())
	require.NotNil(t, cfg)
	assert.True(t, cfg.SSRFGuard)
	assert.True(t, cfg.ProxyDNS)

	httpReq4, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	req4 := aoni.NewStdRequest(httpReq4)
	mod.WithContext(t.Context())(req4)
	assert.Equal(t, t.Context(), req4.Context())
}
