// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/option"
)

func TestReAuthMiddleware_SingleflightConcurrent(t *testing.T) {
	t.Parallel()

	var (
		tokenVal    atomic.Value
		refreshRuns atomic.Int32
	)

	tokenVal.Store("initial_token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("Authorization")
		if tok == "valid_refreshed_token" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}

		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	refreshFn := func(_ context.Context) error {
		refreshRuns.Add(1)
		time.Sleep(50 * time.Millisecond) // Simulate network delay
		tokenVal.Store("valid_refreshed_token")

		return nil
	}

	reauthMid := middleware.ReAuth(middleware.ReAuthConfig{
		Trigger: func(resp aoni.Response, _ error) bool {
			return resp != nil && resp.StatusCode() == http.StatusUnauthorized
		},
		Refresh: refreshFn,
	})

	tokenInjectorMid := func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			req.SetHeader("Authorization", tokenVal.Load().(string))

			return next.Do(req)
		})
	}

	client := aoni.NewClient(server.Client(),
		option.WithBaseURL(server.URL),
		option.WithMiddleware(reauthMid, tokenInjectorMid),
	)

	const concurrency = 10

	var wg sync.WaitGroup
	wg.Add(concurrency)

	startGate := make(chan struct{})
	errs := make([]error, concurrency)

	for i := range concurrency {
		go func(idx int) {
			defer wg.Done()

			<-startGate

			resp, err := client.Raw().Get(t.Context(), "/")
			if err != nil {
				errs[idx] = err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errs[idx] = errors.New("status not ok")
			}
		}(i)
	}

	close(startGate)
	wg.Wait()

	for i, err := range errs {
		require.NoErrorf(t, err, "worker %d failed", i)
	}

	// Exactly 1 refresh must run despite 10 concurrent requests failing simultaneously
	assert.Equal(t, int32(1), refreshRuns.Load())
}

func TestReAuthMiddleware_RefreshFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	refreshFn := func(_ context.Context) error {
		return errors.New("refresh token revoked")
	}

	reauthMid := middleware.ReAuth(middleware.ReAuthConfig{
		Trigger: func(resp aoni.Response, _ error) bool {
			return resp != nil && resp.StatusCode() == http.StatusUnauthorized
		},
		Refresh: refreshFn,
	})

	client := aoni.NewClient(server.Client(),
		option.WithBaseURL(server.URL),
		option.WithMiddleware(reauthMid),
	)

	_, err := client.Raw().Get(t.Context(), "/")
	require.Error(t, err)
	assert.ErrorIs(t, err, middleware.ErrReAuthFailed)
}
