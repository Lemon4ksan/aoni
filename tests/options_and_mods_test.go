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
	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

func TestOptions_Coverage(t *testing.T) {
	t.Parallel()

	cfg := aoni.Config{
		Defaults: aoni.ClientDefaults{
			Headers: make(http.Header),
		},
	}

	option.WithConfig(aoni.Config{})(&cfg)
	option.WithDefaultsBlock(aoni.ClientDefaults{})(&cfg)
	option.WithNetworkBlock(aoni.NetworkConfig{})(&cfg)
	option.WithFingerprintBlock(aoni.FingerprintConfig{})(&cfg)
	option.WithHeader("X-Key", "Val")(&cfg)
	option.WithHeaders(map[string]string{"K1": "V1"})(&cfg)
	option.WithoutHeaders()(&cfg)
	option.WithTimeout(10 * time.Second)(&cfg)
	option.WithHTTP2Config(aoni.HTTP2Config{})(&cfg)
	option.WithUserAgent("CustomUA")(&cfg)
	option.WithOrigin("https://origin.local")(&cfg)
	option.WithBearer("token123")(&cfg)
	option.WithBasicAuth("user", "pass")(&cfg)
	option.WithConnectionPool(aoni.ConnectionPoolConfig{})(&cfg)
	option.WithSettings(h2.Settings{})(&cfg)
	option.WithH2FramedTransport(h2.Settings{})(&cfg)
	option.WithP0fSignature(p0f.Windows10)(&cfg)
	option.WithProxyDNS()(&cfg)
	option.WithCertificatePins(map[string][]string{"example.com": {"pin1"}})(&cfg)

	assert.Equal(t, "CustomUA", cfg.Defaults.Headers.Get("User-Agent"))
	assert.Equal(t, "https://origin.local", cfg.Defaults.Headers.Get("Origin"))
}

func TestModifiers_Coverage(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.com/users/{id}", nil)
	require.NoError(t, err)

	mod.WithVar("id", 123)(req)
	assert.Equal(t, "/users/123", req.URL.Path)

	req2, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.com/item/{cat}/{id}", nil)
	mod.WithVars("cat", "books", "id", 42)(req2)
	assert.Equal(t, "/item/books/42", req2.URL.Path)

	req3, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com", nil)
	mod.WithHeaders(map[string]string{"X-A": "1", "X-B": "2"})(req3)
	assert.Equal(t, "1", req3.Header.Get("X-A"))

	mod.ResetHeaders()(req3)
	assert.Empty(t, req3.Header.Get("X-A"))

	mod.WithBearer("secret")(req3)
	assert.Equal(t, "Bearer secret", req3.Header.Get("Authorization"))

	mod.WithBasicAuth("admin", "pass")(req3)
	assert.Contains(t, req3.Header.Get("Authorization"), "Basic")

	mod.WithUserAgent("AgentX")(req3)
	assert.Equal(t, "AgentX", req3.Header.Get("User-Agent"))

	mod.WithCookie(&http.Cookie{Name: "c1", Value: "v1"})(req3)
	mod.WithCookies(map[string]string{"c2": "v2"})(req3)

	mod.WithJSONBody(map[string]string{"foo": "bar"})(req3)
	assert.Equal(t, "application/json", req3.Header.Get("Content-Type"))

	mod.WithOrigin("https://test.com")(req3)
	assert.Equal(t, "https://test.com", req3.Header.Get("Origin"))

	type formPayload struct {
		Field string `url:"field"`
	}
	mod.WithFormBody(formPayload{Field: "value"})(req3)
	assert.Equal(t, "application/x-www-form-urlencoded", req3.Header.Get("Content-Type"))

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

	req4, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	mod.WithContext(t.Context())(req4)
	assert.Equal(t, t.Context(), req4.Context())
}
