// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inspector_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/inspector"
)

func TestTrafficInspector_CaptureAndHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Response-Header", "response-val-123")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello world"))
	}))
	defer server.Close()

	client := aoni.NewClient(nil)

	client, ins, err := inspector.Enable(client, "127.0.0.1:0")
	require.NoError(t, err)
	require.NotNil(t, ins)

	t.Cleanup(func() {
		_ = ins.Close()
	})

	resp, err := client.Request(context.Background(), http.MethodGet, server.URL+"/test-path", func(r *http.Request) {
		r.Header.Set("X-Custom-Request-Header", "request-val-987")
	})
	require.NoError(t, err)

	_ = resp.Body.Close()

	requests := ins.GetRequests()
	require.Len(t, requests, 1)

	captured := requests[0]
	assert.Equal(t, http.MethodGet, captured.Method)
	assert.Contains(t, captured.URL, "/test-path")
	assert.Equal(t, http.StatusOK, captured.Status)
	assert.Equal(t, "request-val-987", captured.RequestHeaders["X-Custom-Request-Header"])
	assert.Equal(t, "response-val-123", captured.ResponseHeaders["X-Custom-Response-Header"])
}
