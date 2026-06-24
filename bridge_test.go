// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStdClient_BasicGET(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/test", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	t.Cleanup(server.Close)

	c := NewClient(nil).WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	resp, err := stdClient.Get(server.URL + "/test")
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(body))
}

func TestNewStdClient_BasicPOST(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, `{"key":"value"}`, string(body))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	c := NewClient(nil).WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		server.URL+"/submit",
		strings.NewReader(`{"key":"value"}`),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := stdClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
}

func TestWithContextModifier_CarryHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "secret-token", r.Header.Get("Authorization"))
		assert.Equal(t, "custom-value", r.Header.Get("X-Custom"))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	c := NewClient(nil).WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	ctx := WithContextModifier(
		context.Background(),
		WithHeader("Authorization", "secret-token"),
		WithHeader("X-Custom", "custom-value"),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/auth", nil)
	require.NoError(t, err)

	resp, err := stdClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestContextModifiers_Empty(t *testing.T) {
	t.Parallel()

	mods := ContextModifiers(context.Background())
	assert.Nil(t, mods)
}

func TestContextModifiers_Multiple(t *testing.T) {
	t.Parallel()

	called := make([]string, 0, 3)

	m1 := func(req *http.Request) { called = append(called, "m1") }
	m2 := func(req *http.Request) { called = append(called, "m2") }
	m3 := func(req *http.Request) { called = append(called, "m3") }

	ctx := WithContextModifier(context.Background(), m1, m2, m3)
	mods := ContextModifiers(ctx)

	require.Len(t, mods, 3)

	for _, mod := range mods {
		mod(nil)
	}

	assert.Equal(t, []string{"m1", "m2", "m3"}, called)
}

func TestNewStdClient_CookieJarNil(t *testing.T) {
	t.Parallel()

	c := NewClient(nil)
	stdClient := NewStdClient(c)

	assert.Nil(t, stdClient.Jar)
}

func TestBridge_ParallelRequests(t *testing.T) {
	t.Parallel()

	var count atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	c := NewClient(nil).WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	var wg sync.WaitGroup

	for i := range 10 {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			resp, err := stdClient.Get(server.URL + "/parallel")
			require.NoError(t, err)
			t.Cleanup(func() { resp.Body.Close() })

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, "ok", string(body))
		}(i)
	}

	wg.Wait()
	assert.Equal(t, int64(10), count.Load())
}

func TestBridge_WithRetryMiddleware(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	t.Cleanup(server.Close)

	retryOpts := RetryOptions{
		MaxRetries:     5,
		Backoff:        0,
		JitterStrategy: JitterEqual,
	}

	doer := Chain(
		&http.Client{},
		RetryMiddleware(retryOpts, RetryOnGatewayErrors()),
	)

	c := NewClient(doer).WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	resp, err := stdClient.Get(server.URL + "/retry")
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "success", string(body))
	assert.GreaterOrEqual(t, attempts.Load(), int32(3))
}

func TestBridge_WithCircuitBreaker(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 0.5,
		MinRequests:      2,
		Cooldown:         0,
		Window:           10 * time.Second,
	})

	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	doer := Chain(
		&http.Client{},
		CircuitBreakerMiddleware(cb, DefaultCircuitBreakerCondition),
	)

	c := NewClient(doer).WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	// Fire enough requests to trigger the breaker
	for range 10 {
		resp, err := stdClient.Get(server.URL + "/cb")
		if err != nil {
			break
		}

		resp.Body.Close()
	}

	// Verify some requests were made
	assert.Greater(t, attempts.Load(), int32(0))
}

func TestBridge_ContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	c := NewClient(nil).WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/cancel", nil)
	require.NoError(t, err)

	_, err = stdClient.Do(req)
	assert.Error(t, err)
}

func TestBridge_HeadersPreserved(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Multi-value header
		assert.Equal(t, []string{"v1", "v2"}, r.Header.Values("X-Multi"))
		// Custom user-agent
		assert.Equal(t, "test-agent/1.0", r.Header.Get("User-Agent"))

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	c := NewClient(nil).WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		server.URL+"/headers",
		nil,
	)
	require.NoError(t, err)

	req.Header.Add("X-Multi", "v1")
	req.Header.Add("X-Multi", "v2")
	req.Header.Set("User-Agent", "test-agent/1.0")

	resp, err := stdClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestBridge_BodyStreamPreserved(t *testing.T) {
	t.Parallel()

	const payload = "streaming body content with special chars: àáâãäå"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, payload, string(body))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("received"))
	}))
	t.Cleanup(server.Close)

	c := NewClient(nil).WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		server.URL+"/stream",
		strings.NewReader(payload),
	)
	require.NoError(t, err)

	resp, err := stdClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "received", string(body))
}

func TestBridge_QueryParamsPreserved(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "bar", r.URL.Query().Get("foo"))
		assert.Equal(t, "z spaces", r.URL.Query().Get("baz"))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	c := NewClient(nil).WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		server.URL+"/query?foo=bar&baz=z+spaces",
		nil,
	)
	require.NoError(t, err)

	resp, err := stdClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestBridge_StatusCodes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/200":
			w.WriteHeader(http.StatusOK)
		case "/404":
			w.WriteHeader(http.StatusNotFound)
		case "/500":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)

	c := NewClient(nil).WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	tests := []struct {
		path           string
		expectedStatus int
	}{
		{"/200", http.StatusOK},
		{"/404", http.StatusNotFound},
		{"/500", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			resp, err := stdClient.Get(server.URL + tt.path)
			require.NoError(t, err)
			resp.Body.Close()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode)
		})
	}
}

func TestBridge_LargeResponseBody(t *testing.T) {
	t.Parallel()

	largeData := strings.Repeat("A", 100*1024)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(largeData))
	}))
	t.Cleanup(server.Close)

	c := NewClient(nil).WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	resp, err := stdClient.Get(server.URL + "/large")
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, 100*1024, len(body))
	assert.Equal(t, largeData, string(body))
}

func TestBridge_GzipResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/plain")

		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte("gzipped content"))
		_ = gz.Close()
	}))
	t.Cleanup(server.Close)

	c := NewClient(nil).WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	resp, err := stdClient.Get(server.URL + "/gzip")
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "gzipped content", string(body))
}

func TestWithContextModifier_Empty(t *testing.T) {
	t.Parallel()

	ctx := WithContextModifier(context.Background())
	mods := ContextModifiers(ctx)
	assert.Nil(t, mods)
}
