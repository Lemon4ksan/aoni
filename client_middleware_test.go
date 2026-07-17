// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserAgentAndHintsRotation(t *testing.T) {
	t.Parallel()

	profiles := []BrowserProfile{
		{
			UserAgent: "BrowserA",
			ClientHints: map[string]string{
				"Sec-CH-UA": "BrandA",
			},
		},
		{
			UserAgent: "BrowserB",
			ClientHints: map[string]string{
				"Sec-CH-UA": "BrandB",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-UA", r.Header.Get("User-Agent"))
		w.Header().Set("X-Hint", r.Header.Get("Sec-CH-UA"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(nil,
		WithClientBaseURL(server.URL),
		WithClientPipelineWrapper(func(c *Client, engine HTTPDoer) HTTPDoer {
			return Chain(engine, UserAgentAndHintsRotationMiddleware(profiles))
		}),
	)

	resp1, err := client.Get(t.Context(), "/")
	require.NoError(t, err)

	defer resp1.Body.Close()

	assert.Equal(t, "BrowserA", resp1.Header.Get("X-UA"))
	assert.Equal(t, "BrandA", resp1.Header.Get("X-Hint"))

	resp2, err := client.Get(t.Context(), "/")
	require.NoError(t, err)

	defer resp2.Body.Close()

	assert.Equal(t, "BrowserB", resp2.Header.Get("X-UA"))
	assert.Equal(t, "BrandB", resp2.Header.Get("X-Hint"))
}

func TestDPIJitterMiddleware(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(nil,
		WithClientBaseURL(server.URL),
		WithClientPipelineWrapper(func(c *Client, engine HTTPDoer) HTTPDoer {
			return Chain(engine, DPIJitterMiddleware(10*time.Millisecond, 20*time.Millisecond))
		}),
	)

	start := time.Now()
	resp, err := client.Get(t.Context(), "/")
	require.NoError(t, err)

	defer resp.Body.Close()

	duration := time.Since(start)

	assert.GreaterOrEqual(t, duration, 10*time.Millisecond)
}

func TestProxyFailoverMiddleware(t *testing.T) {
	t.Parallel()

	var attempts int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := NewClient(nil,
		WithClientBaseURL(server.URL),
		WithClientPipelineWrapper(func(c *Client, engine HTTPDoer) HTTPDoer {
			// First proxy fails immediately (connection refused), second is the working server
			proxies := []string{"http://127.0.0.1:9999", server.URL}
			return Chain(engine, ProxyFailoverMiddleware(proxies, 2))
		}),
	)

	resp, err := client.Get(t.Context(), "/")
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
	assert.Equal(t, 1, attempts)
}

func TestCacheMiddleware(t *testing.T) {
	t.Parallel()

	var hits int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++

		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("cached content"))
	}))
	defer server.Close()

	store := NewInMemoryCacheStore()
	client := NewClient(nil,
		WithClientBaseURL(server.URL),
		WithClientPipelineWrapper(func(c *Client, engine HTTPDoer) HTTPDoer {
			return Chain(engine, CacheMiddleware(store, 1*time.Minute))
		}),
	)

	resp1, err := client.Get(t.Context(), "/")
	require.NoError(t, err)

	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()
	assert.Equal(t, "cached content", string(body1))

	resp2, err := client.Get(t.Context(), "/")
	require.NoError(t, err)

	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	assert.Equal(t, "cached content", string(body2))

	assert.Equal(t, 1, hits) // Only 1 request hit the server, second was cached
}

type mockInspector struct {
	capturedReq  *http.Request
	capturedResp *http.Response
}

func (m *mockInspector) Capture(req *http.Request, resp *http.Response, err error, traceInfo *TraceInfo) {
	m.capturedReq = req
	m.capturedResp = resp
}

func TestSensitiveDataRedactor(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "secret-session")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	inspector := &mockInspector{}
	client := NewClient(nil,
		WithClientBaseURL(server.URL),
		WithClientPipelineWrapper(func(c *Client, engine HTTPDoer) HTTPDoer {
			// Redactor runs before Inspector downstream (leftmost executes first)
			return Chain(engine,
				SensitiveDataRedactorMiddleware([]string{"Authorization", "Set-Cookie"}, nil),
				InspectorMiddleware(inspector),
			)
		}),
	)

	resp, err := client.Request(t.Context(), "GET", "/", WithHeader("Authorization", "Bearer secretToken"))
	require.NoError(t, err)

	defer resp.Body.Close()

	// Verify headers are marked for redaction in the context of the captured request
	cfg, ok := inspector.capturedReq.Context().Value(RedactConfigCtxKey{}).(*RedactConfig)
	require.True(t, ok)
	require.NotNil(t, cfg)
	assert.True(t, cfg.Headers["authorization"])
	assert.True(t, cfg.Headers["set-cookie"])
}

func TestHARGenerator(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("har body"))
	}))
	defer server.Close()

	harGen := NewHARGenerator()
	client := NewClient(nil,
		WithClientBaseURL(server.URL),
		WithClientPipelineWrapper(func(c *Client, engine HTTPDoer) HTTPDoer {
			return Chain(engine, HARGeneratorMiddleware(harGen))
		}),
	)

	resp, err := client.Get(t.Context(), "/")
	require.NoError(t, err)

	defer resp.Body.Close()

	data, err := harGen.Export()
	require.NoError(t, err)

	harString := string(data)
	assert.Contains(t, harString, "har body")
	assert.Contains(t, harString, "GET")
	assert.Contains(t, harString, server.URL)
}
