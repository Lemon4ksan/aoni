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

	"github.com/lemon4ksan/foundation/borrow"
	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/internal/std"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

func TestClient_Do_NilRequest(t *testing.T) {
	t.Parallel()

	fc := fast.NewClient()
	_, err := fc.Do(nil)
	require.ErrorIs(t, err, fast.ErrNilRequest)
}

func TestClient_Do_StandardRequestAdapter(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

func TestClient_MethodParity_AllMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Received-Method", r.Method)
		w.Header().Set("X-Received-Custom", r.Header.Get("X-Custom"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("method:" + r.Method + ",body:" + string(bodyBytes)))
	}))
	defer server.Close()

	fc := fast.NewClient()
	defer fc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Get
	t.Run("Get", func(t *testing.T) {
		resp, err := fc.Get(ctx, server.URL, mod.WithHeader("X-Custom", "val-get"))
		require.NoError(t, err)

		defer resp.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode())
		assert.Equal(t, "GET", resp.Header("X-Received-Method"))
		assert.Equal(t, "val-get", resp.Header("X-Received-Custom"))
	})

	// 2. Post (JSON struct auto-detection)
	t.Run("Post_JSON", func(t *testing.T) {
		payload := map[string]string{"foo": "bar"}
		resp, err := fc.Post(ctx, server.URL, payload)
		require.NoError(t, err)

		defer resp.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode())
		assert.Equal(t, "POST", resp.Header("X-Received-Method"))
		assert.Contains(t, string(resp.BodyBytes()), `{"foo":"bar"}`)
	})

	// 3. Put (raw bytes)
	t.Run("Put_Bytes", func(t *testing.T) {
		resp, err := fc.Put(ctx, server.URL, []byte("raw-put"))
		require.NoError(t, err)

		defer resp.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode())
		assert.Equal(t, "PUT", resp.Header("X-Received-Method"))
		assert.Contains(t, string(resp.BodyBytes()), "raw-put")
	})

	// 4. Delete
	t.Run("Delete", func(t *testing.T) {
		resp, err := fc.Delete(ctx, server.URL)
		require.NoError(t, err)

		defer resp.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode())
		assert.Equal(t, "DELETE", resp.Header("X-Received-Method"))
	})

	// 5. Patch
	t.Run("Patch", func(t *testing.T) {
		resp, err := fc.Patch(ctx, server.URL, "patch-payload")
		require.NoError(t, err)

		defer resp.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode())
		assert.Equal(t, "PATCH", resp.Header("X-Received-Method"))
		assert.Contains(t, string(resp.BodyBytes()), "patch-payload")
	})

	// 6. Head
	t.Run("Head", func(t *testing.T) {
		resp, err := fc.Head(ctx, server.URL)
		require.NoError(t, err)

		defer resp.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode())
		assert.Equal(t, "HEAD", resp.Header("X-Received-Method"))
	})

	// 7. Options
	t.Run("Options", func(t *testing.T) {
		resp, err := fc.Options(ctx, server.URL)
		require.NoError(t, err)

		defer resp.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode())
		assert.Equal(t, "OPTIONS", resp.Header("X-Received-Method"))
	})

	// 8. Fetch (Custom Method)
	t.Run("Fetch_Custom", func(t *testing.T) {
		resp, err := fc.Fetch(ctx, "SEARCH", server.URL, "search-body")
		require.NoError(t, err)

		defer resp.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode())
		assert.Equal(t, "SEARCH", resp.Header("X-Received-Method"))
		assert.Contains(t, string(resp.BodyBytes()), "search-body")
	})
}

func TestClient_Middleware_Use(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "intercepted", r.Header.Get("X-Intercepted"))
		w.Header().Set("X-Server", "ok")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mw-response"))
	}))
	defer server.Close()

	fc := fast.NewClient()
	defer fc.Close()

	var order []string

	mw1 := func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			order = append(order, "mw1-before")

			req.SetHeader("X-Intercepted", "intercepted")
			resp, err := next.Do(req)

			order = append(order, "mw1-after")

			return resp, err
		})
	}
	mw2 := func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			order = append(order, "mw2-before")
			resp, err := next.Do(req)

			order = append(order, "mw2-after")

			return resp, err
		})
	}

	fc.Use(mw1, mw2)

	resp, err := fc.Get(context.Background(), server.URL)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, []string{"mw1-before", "mw2-before", "mw2-after", "mw1-after"}, order)
}

func TestClient_Middleware_Builtin_Retry(t *testing.T) {
	t.Parallel()

	var attempts int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("temporary failure"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success on retry"))
	}))
	defer server.Close()

	fc := fast.NewClient()
	defer fc.Close()

	retryMw := middleware.Retry(
		middleware.RetryOptions{
			MaxAttempts: 4,
		},
		func(resp aoni.Response, err error) bool {
			return resp != nil && resp.StatusCode() == http.StatusServiceUnavailable
		},
	)
	fc.Use(retryMw)

	resp, err := fc.Get(context.Background(), server.URL)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, 3, attempts)
	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, "success on retry", string(resp.BodyBytes()))
}

func TestClient_With_Clone(t *testing.T) {
	t.Parallel()

	fc := fast.NewClient(option.WithBaseURL("https://example.com"))
	fc2 := fc.With(option.WithHeader("X-Custom", "abc"))

	assert.Equal(t, "https://example.com/", fc.Config().Defaults.BaseURL.String())
	assert.Equal(t, "https://example.com/", fc2.Config().Defaults.BaseURL.String())
	assert.Equal(t, "abc", fc2.Config().Defaults.Headers.Get("X-Custom"))
	assert.Empty(t, fc.Config().Defaults.Headers.Get("X-Custom"))

	cloned := fc2.Clone()
	assert.Equal(t, "abc", cloned.Config().Defaults.Headers.Get("X-Custom"))
}

func TestClient_ScopedExecution(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("scoped-body"))
	}))
	defer server.Close()

	fc := fast.NewClient()
	defer fc.Close()

	err := fc.RequestScoped(
		context.Background(),
		http.MethodGet,
		server.URL,
		func(s *borrow.Scope, resp *fast.Response) error {
			body := resp.BodyScoped(s)
			assert.Equal(t, "scoped-body", string(body.Bytes()))
			return nil
		},
	)
	require.NoError(t, err)
}
