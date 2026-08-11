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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/option"
)

func TestFastClient_BasicGet(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/users", r.URL.Path)
		assert.Equal(t, "test-agent", r.Header.Get("User-Agent"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	client := fast.NewClient(
		option.WithBaseURL(ts.URL),
		option.WithUserAgent("test-agent"),
	)
	defer client.CloseIdleConnections()

	req := fast.NewRequest(nil)
	defer req.Release()

	req.SetContext(context.Background())
	req.SetMethod("GET")
	req.SetURL(ts.URL + "/api/v1/users")
	req.SetHeader("User-Agent", "test-agent")

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())

	body := resp.BodyBytes()
	assert.JSONEq(t, `{"status":"ok"}`, string(body))
}

func TestFastClient_PostJSON(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		assert.JSONEq(t, `{"name":"alice"}`, string(body))

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":123}`))
	}))
	defer ts.Close()

	client := fast.NewClient(option.WithBaseURL(ts.URL))
	defer client.CloseIdleConnections()

	req := fast.NewRequest(nil)
	defer req.Release()

	req.SetContext(context.Background())
	req.SetMethod("POST")
	req.SetURL(ts.URL + "/users")
	req.SetHeader("Content-Type", "application/json")
	req.SetBodyBytes([]byte(`{"name":"alice"}`))

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode())
}

func TestFastClient_TimeoutHandling(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := fast.NewClient(option.WithBaseURL(ts.URL))
	defer client.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	req := fast.NewRequest(nil)
	defer req.Release()

	req.SetContext(ctx)
	req.SetMethod("GET")
	req.SetURL(ts.URL + "/")

	_, err := client.Do(req)
	assert.Error(t, err)
}
