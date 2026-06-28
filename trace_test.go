// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license[s] that can be found in the LICENSE file.

package aoni

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrace(t *testing.T) {
	t.Parallel()

	t.Run("capture_trace_info_and_remote_addr", func(t *testing.T) {
		t.Parallel()
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok"))
		})

		var traceInfo TraceInfo

		resp, err := client.Request(t.Context(), http.MethodGet, "/trace", Trace(&traceInfo))
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		_, _ = io.ReadAll(resp.Body)

		assert.GreaterOrEqual(t, traceInfo.ServerProcessing, time.Duration(0))
		assert.NotEmpty(t, traceInfo.RemoteAddr)
		assert.Contains(t, traceInfo.RemoteAddr, "127.0.0.1")
	})
}

func TestTraceInfo_Start_CalculatesTransfer(t *testing.T) {
	t.Parallel()

	info := &TraceInfo{
		DNSLookup:        5 * time.Millisecond,
		TCPConn:          10 * time.Millisecond,
		TLSHandshake:     15 * time.Millisecond,
		ServerProcessing: 20 * time.Millisecond,
	}

	finish := info.Start()

	time.Sleep(60 * time.Millisecond) // Simulate body read delay

	resp := &http.Response{
		ContentLength: 1024,
	}

	finish(resp)

	assert.Greater(t, info.Total, 50*time.Millisecond)
	assert.Equal(t, int64(1024), info.ResponseSize)
	assert.Greater(t, info.ContentTransfer, time.Duration(0))
}

func TestTraceJA4(t *testing.T) {
	t.Parallel()

	t.Run("generate_ja4h_fingerprint", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(context.Background(), "POST", "http://example.com/api", nil)
		require.NoError(t, err)

		req.Header.Set("User-Agent", "Mozilla/5.0")
		req.Header.Set("Accept-Language", "en-US")
		req.AddCookie(&http.Cookie{Name: "session", Value: "abc"})

		var traceInfo TraceInfo

		mod := TraceJA4(&traceInfo)
		mod(req)

		require.NotNil(t, traceInfo.JA4)
		assert.NotEmpty(t, traceInfo.JA4.JA4H)
		assert.Contains(t, traceInfo.JA4.JA4H, "po11")
	})
}

func TestCurlCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		method  string
		url     string
		headers map[string]string
		body    []byte
		want    []string
		notWant []string
	}{
		{
			name:    "simple_get_request",
			method:  "GET",
			url:     "http://example.com/api/test",
			headers: map[string]string{"Authorization": "Bearer token123"},
			body:    nil,
			want:    []string{"curl", "http://example.com/api/test", "Authorization: Bearer token123"},
		},
		{
			name:    "post_request_with_body",
			method:  "POST",
			url:     "http://example.com/api/test",
			headers: map[string]string{"Content-Type": "application/json"},
			body:    []byte(`{"key": "value"}`),
			want:    []string{"-X POST", "-d '{\"key\": \"value\"}'", "Content-Type: application/json"},
		},
		{
			name:    "get_request_no_method_flag",
			method:  "GET",
			url:     "http://example.com/api/test",
			headers: nil,
			body:    nil,
			want:    []string{"http://example.com/api/test"},
			notWant: []string{"-X GET"},
		},
		{
			name:    "request_with_multiple_headers",
			method:  "GET",
			url:     "http://example.com/api/test",
			headers: map[string]string{"X-Custom1": "value1", "X-Custom2": "value2"},
			body:    nil,
			want:    []string{"X-Custom1: value1", "X-Custom2: value2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequestWithContext(t.Context(), tt.method, tt.url, nil)
			require.NoError(t, err)

			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			curl := CurlCommand(req, tt.body)
			for _, w := range tt.want {
				assert.Contains(t, curl, w)
			}

			for _, nw := range tt.notWant {
				assert.NotContains(t, curl, nw)
			}
		})
	}
}

func TestAsCurl_WithBody(t *testing.T) {
	t.Parallel()

	server, client := setupBridgeTest(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, "replayed_body_data", string(body))
		w.WriteHeader(http.StatusOK)
	})

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		server.URL+"/curl",
		strings.NewReader("replayed_body_data"),
	)
	require.NoError(t, err)

	// Apply AsCurl modifier (captures and re-populates the body)
	mod := AsCurl()
	mod(req)

	resp, err := client.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
