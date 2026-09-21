// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/internal/std"
)

func TestClient_Do_NilRequest(t *testing.T) {
	fc := fast.NewClient()
	_, err := fc.Do(nil)
	require.ErrorIs(t, err, fast.ErrNilRequest)
}

func TestClient_Do_StandardRequestAdapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-val", r.Header.Get("X-Custom-Header"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	fc := fast.NewClient()
	defer fc.Engine().CloseIdleConnections()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	req.Header.Set("X-Custom-Header", "test-val")

	stdReq := std.NewRequest(req)
	resp, err := fc.Do(stdReq)
	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, `{"status":"ok"}`, string(resp.BodyBytes()))
}

func TestAoniClient_WithFastClient_Engine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello from server"))
	}))
	defer server.Close()

	fc := fast.NewClient()
	defer fc.Engine().CloseIdleConnections()

	client := aoni.NewClient(fc)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Get(ctx, server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "hello from server", string(body))
}
