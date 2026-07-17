// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProxyOverride_Set(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	WithProxyOverride("http://proxy.local:8080")(req)

	raw, ok := GetProxyOverride(req.Context()).Value()
	require.True(t, ok)
	assert.Equal(t, "http://proxy.local:8080", raw)
}

func TestGetProxyOverride_NotSet(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	_, ok := GetProxyOverride(req.Context()).Value()
	assert.False(t, ok)
}

func TestProxyFuncWithOverride_PreferOverride(t *testing.T) {
	base := http.ProxyURL(&url.URL{Scheme: "http", Host: "global-proxy:8080"})
	wrapped := ProxyFuncWithOverride(base)

	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	WithProxyOverride("http://per-request-proxy:9090")(req)

	u, err := wrapped(req)
	require.NoError(t, err)
	assert.Equal(t, "per-request-proxy:9090", u.Host)
}

func TestProxyFuncWithOverride_FallbackToBase(t *testing.T) {
	base := http.ProxyURL(&url.URL{Scheme: "http", Host: "global-proxy:8080"})
	wrapped := ProxyFuncWithOverride(base)

	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)

	u, err := wrapped(req)
	require.NoError(t, err)
	assert.Equal(t, "global-proxy:8080", u.Host)
}

func TestProxyFuncWithOverride_NilBase(t *testing.T) {
	wrapped := ProxyFuncWithOverride(nil)

	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)

	u, err := wrapped(req)
	require.NoError(t, err)
	assert.Nil(t, u)
}

func TestGetInsecureSkipVerify_Set(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	WithInsecureSkipVerify()(req)
	assert.True(t, GetInsecureSkipVerify(req.Context()))
}

func TestGetInsecureSkipVerify_NotSet(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	assert.False(t, GetInsecureSkipVerify(req.Context()))
}

func TestTLSConfigWithOverride_AppliesFlag(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	WithInsecureSkipVerify()(req)

	cfg := TLSConfigWithOverride(req.Context(), nil)
	require.NotNil(t, cfg)
	assert.True(t, cfg.InsecureSkipVerify)
}

func TestTLSConfigWithOverride_NoCloneWhenNotSet(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.com", nil)
	// not calling WithInsecureSkipVerify()
	assert.False(t, GetInsecureSkipVerify(req.Context()))
}

func TestGetTCPDelay_Set(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	WithTCPDelay(10*time.Millisecond, 20*time.Millisecond)(req)

	r, ok := GetTCPDelay(req.Context()).Value()
	require.True(t, ok)
	assert.Equal(t, 10*time.Millisecond, r.Min)
	assert.Equal(t, 20*time.Millisecond, r.Max)
}

func TestWithTCPDelay_SwapsMinMax(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	WithTCPDelay(50*time.Millisecond, 10*time.Millisecond)(req) // reversed

	r, ok := GetTCPDelay(req.Context()).Value()
	require.True(t, ok)
	assert.LessOrEqual(t, r.Min, r.Max, "min must be <= max after swap")
}

func TestApplyTCPDelay_NoDelay(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	// No delay set should return immediately.
	start := time.Now()
	err := ApplyTCPDelay(req.Context())
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 10*time.Millisecond)
}

func TestApplyTCPDelay_WithDelay(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	WithTCPDelay(20*time.Millisecond, 30*time.Millisecond)(req)

	start := time.Now()
	err := ApplyTCPDelay(req.Context())
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, 20*time.Millisecond, "delay must be at least min")
}

func TestConnMetadata_SetAndGet(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	WithConnMetadata("proxy-id", "proxy-42")(req)

	val, ok := GetConnMetadata(req.Context(), "proxy-id").Value()
	require.True(t, ok)
	assert.Equal(t, "proxy-42", val)
}

func TestConnMetadata_MultipleKeys(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	WithConnMetadata("pool", "eu-west")(req)
	WithConnMetadata("trace-id", "abc123")(req)

	pool, ok1 := GetConnMetadata(req.Context(), "pool").Value()
	trace, ok2 := GetConnMetadata(req.Context(), "trace-id").Value()

	require.True(t, ok1)
	require.True(t, ok2)
	assert.Equal(t, "eu-west", pool)
	assert.Equal(t, "abc123", trace)
}

func TestConnMetadata_MissingKey(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	_, ok := GetConnMetadata(req.Context(), "nonexistent").Value()
	assert.False(t, ok)
}

func TestWithResponseValidator_PassesOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Status", "ok")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient(nil, WithClientBaseURL(srv.URL))

	resp, err := c.Get(t.Context(), "/",
		WithResponseValidator(func(resp *http.Response) error {
			if resp.Header.Get("X-Status") != "ok" {
				return errors.New("missing X-Status")
			}

			return nil
		}),
	)
	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = resp.Body.Close()
}

func TestWithResponseValidator_BlocksOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Access Denied"))
	}))
	defer srv.Close()

	c := NewClient(nil, WithClientBaseURL(srv.URL))

	resp, err := c.Get(t.Context(), "/",
		WithResponseValidator(func(resp *http.Response) error {
			body, _ := io.ReadAll(resp.Body)

			resp.Body = io.NopCloser(bytes.NewReader(body)) // reset for caller
			if strings.Contains(string(body), "Access Denied") {
				return errors.New("aoni: access denied by validator")
			}

			return nil
		}),
	)
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "access denied by validator")
}

func TestGetCacheTTL_Set(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	WithCacheTTL(5 * time.Minute)(req)

	d, ok := GetCacheTTL(req.Context()).Value()
	require.True(t, ok)
	assert.Equal(t, 5*time.Minute, d)
}

func TestGetCacheTTL_NotSet(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	_, ok := GetCacheTTL(req.Context()).Value()
	assert.False(t, ok)
}

func TestGetRetryOverride_Set(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	WithRetryPolicy(RetryOverride{
		MaxAttempts: 5,
		Backoff:     200 * time.Millisecond,
	})(req)

	o, ok := GetRetryOverride(req.Context()).Value()
	require.True(t, ok)
	assert.Equal(t, 5, o.MaxAttempts)
	assert.Equal(t, 200*time.Millisecond, o.Backoff)
	assert.NotNil(t, o.Condition)
}

func TestGetRetryOverride_DefaultsMaxAttempts(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	WithRetryPolicy(RetryOverride{MaxAttempts: 0})(req) // should clamp to 1

	o, ok := GetRetryOverride(req.Context()).Value()
	require.True(t, ok)
	assert.Equal(t, 1, o.MaxAttempts)
}

func TestGetRetryOverride_NotSet(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	_, ok := GetRetryOverride(req.Context()).Value()
	assert.False(t, ok)
}
