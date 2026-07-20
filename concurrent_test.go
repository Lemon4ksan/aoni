// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithTimeout(t *testing.T) {
	t.Parallel()

	t.Run("request_completes_within_timeout", func(t *testing.T) {
		t.Parallel()
		_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"message":"ok","status":200}`)
		})

		res, err := GetTo[reqTestPayload](t.Context(), client, "/", WithTimeout(2*time.Second))
		require.NoError(t, err)
		assert.Equal(t, "ok", res.Message)
	})

	t.Run("request_times_out", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Hang until the client's deadline fires.
			<-r.Context().Done()
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(server.Close)
		client := NewClient(nil, withBaseURL(server.URL))

		_, err := GetTo[reqTestPayload](t.Context(), client, "/slow", WithTimeout(100*time.Millisecond))
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

func TestWithFormValues(t *testing.T) {
	t.Parallel()

	_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		assert.Equal(t, "world", r.FormValue("hello"))
		assert.Equal(t, "42", r.FormValue("answer"))
		w.WriteHeader(http.StatusNoContent)
	})

	resp, err := Post(t.Context(), client, "/form", nil,
		WithFormValues(url.Values{
			"hello":  {"world"},
			"answer": {"42"},
		}),
	)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestConditionalHeaders(t *testing.T) {
	t.Parallel()

	t.Run("WithIfNoneMatch", func(t *testing.T) {
		t.Parallel()
		_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, `"abc123"`, r.Header.Get("If-None-Match"))
			w.WriteHeader(http.StatusNotModified)
		})

		resp, err := Get(t.Context(), client, "/", WithIfNoneMatch(`"abc123"`))
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotModified, resp.StatusCode)
	})

	t.Run("WithIfMatch", func(t *testing.T) {
		t.Parallel()
		_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, `"abc123"`, r.Header.Get("If-Match"))
			w.WriteHeader(http.StatusOK)
		})

		resp, err := Put(t.Context(), client, "/", nil, WithIfMatch(`"abc123"`))
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("WithIfModifiedSince", func(t *testing.T) {
		t.Parallel()

		since := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		_, client := setupTestReqServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, since.UTC().Format(http.TimeFormat), r.Header.Get("If-Modified-Since"))
			w.WriteHeader(http.StatusNotModified)
		})

		resp, err := Get(t.Context(), client, "/", WithIfModifiedSince(since))
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotModified, resp.StatusCode)
	})
}

func TestConcurrent(t *testing.T) {
	t.Parallel()

	paths := []string{"/a", "/b", "/c"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"message":"path:%s","status":200}`, r.URL.Path)
	}))
	t.Cleanup(server.Close)

	client := NewClient(nil, withBaseURL(server.URL))

	results := Concurrent(t.Context(), client, paths,
		func(ctx context.Context, c Requester, path string) (*reqTestPayload, error) {
			return GetTo[reqTestPayload](ctx, c, path)
		})

	require.Len(t, results, 3)

	for _, r := range results {
		require.NoError(t, r.Err)
		require.NotNil(t, r.Value)
		assert.Equal(t, "path:"+paths[r.Index], r.Value.Message)
	}
}

func TestConcurrentWithMods(t *testing.T) {
	t.Parallel()

	paths := []string{"/x", "/y"}
	mods := [][]RequestModifier{
		{WithHeader("X-Custom", "first")},
		{WithHeader("X-Custom", "second")},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"message":"%s","status":200}`, r.Header.Get("X-Custom"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(nil, withBaseURL(server.URL))

	results := ConcurrentWithMods(t.Context(), client, paths, mods,
		func(ctx context.Context, c Requester, path string, m ...RequestModifier) (*reqTestPayload, error) {
			return GetTo[reqTestPayload](ctx, c, path, m...)
		})

	require.Len(t, results, 2)

	for _, r := range results {
		require.NoError(t, r.Err)

		expected := []string{"first", "second"}
		assert.Equal(t, expected[r.Index], r.Value.Message)
	}
}
