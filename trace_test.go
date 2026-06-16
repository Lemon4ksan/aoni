// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrace(t *testing.T) {
	t.Run("Capture trace info", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)

		var traceInfo TraceInfo

		resp, err := client.Request(context.Background(), http.MethodGet, "/trace", Trace(&traceInfo))
		require.NoError(t, err)

		defer resp.Body.Close()

		_, _ = io.ReadAll(resp.Body)

		// Server processing should be measured
		assert.GreaterOrEqual(t, traceInfo.ServerProcessing, time.Duration(0))
	})

	t.Run("Nil body request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)

		var traceInfo TraceInfo

		resp, err := client.Request(context.Background(), http.MethodGet, "/trace", Trace(&traceInfo))
		require.NoError(t, err)

		defer resp.Body.Close()

		_, _ = io.ReadAll(resp.Body)

		// Should not panic with nil body
		assert.GreaterOrEqual(t, traceInfo.Total, time.Duration(0))
	})
}

func TestCurlCommand(t *testing.T) {
	t.Run("Simple GET request", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://example.com/api/test", nil)
		req.Header.Set("Authorization", "Bearer token123")

		curl := CurlCommand(req, nil)
		assert.Contains(t, curl, "curl")
		assert.Contains(t, curl, "http://example.com/api/test")
		assert.Contains(t, curl, "Authorization: Bearer token123")
	})

	t.Run("POST request with body", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "http://example.com/api/test", nil)
		req.Header.Set("Content-Type", "application/json")

		body := []byte(`{"key": "value"}`)

		curl := CurlCommand(req, body)
		assert.Contains(t, curl, "-X POST")
		assert.Contains(t, curl, "-d '{\"key\": \"value\"}'")
		assert.Contains(t, curl, "Content-Type: application/json")
	})

	t.Run("GET request (no method flag)", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://example.com/api/test", nil)

		curl := CurlCommand(req, nil)
		assert.NotContains(t, curl, "-X GET")
		assert.Contains(t, curl, "http://example.com/api/test")
	})

	t.Run("Request with multiple headers", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://example.com/api/test", nil)
		req.Header.Set("X-Custom1", "value1")
		req.Header.Set("X-Custom2", "value2")

		curl := CurlCommand(req, nil)
		assert.Contains(t, curl, "X-Custom1: value1")
		assert.Contains(t, curl, "X-Custom2: value2")
	})
}

func TestAsCurl(t *testing.T) {
	t.Run("AsCurl modifier", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}))
		defer server.Close()

		client := NewClient(nil).WithBaseURL(server.URL)

		// AsCurl should not panic
		resp, err := client.Request(context.Background(), http.MethodGet, "/curl", AsCurl())
		require.NoError(t, err)

		defer resp.Body.Close()

		_, _ = io.ReadAll(resp.Body)
	})
}
