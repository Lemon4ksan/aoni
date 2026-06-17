// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrace(t *testing.T) {
	t.Parallel()

	t.Run("capture_trace_info", func(t *testing.T) {
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
	})

	t.Run("nil_body_request", func(t *testing.T) {
		t.Parallel()
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok"))
		})

		var traceInfo TraceInfo

		resp, err := client.Request(t.Context(), http.MethodGet, "/trace", Trace(&traceInfo))
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		_, _ = io.ReadAll(resp.Body)

		assert.GreaterOrEqual(t, traceInfo.Total, time.Duration(0))
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

func TestAsCurl(t *testing.T) {
	t.Parallel()

	t.Run("as_curl_modifier", func(t *testing.T) {
		t.Parallel()
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok"))
		})

		resp, err := client.Request(t.Context(), http.MethodGet, "/curl", AsCurl())
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		_, _ = io.ReadAll(resp.Body)
	})
}
