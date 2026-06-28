// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
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

type loadMockDoer struct {
	id    int
	calls int32
}

func (m *loadMockDoer) Do(req *http.Request) (*http.Response, error) {
	atomic.AddInt32(&m.calls, 1)

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("prewarmed")),
	}, nil
}

func (m *loadMockDoer) GetCalls() int {
	return int(atomic.LoadInt32(&m.calls))
}

type mockHTTPServer struct {
	server     *httptest.Server
	statusCode int
	mu         sync.RWMutex
}

func newMockHTTPServer(t *testing.T, statusCode int) *mockHTTPServer {
	t.Helper()

	m := &mockHTTPServer{statusCode: statusCode}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		code := m.statusCode
		m.mu.RUnlock()
		w.WriteHeader(code)
	}))

	t.Cleanup(m.server.Close)

	return m
}

func (m *mockHTTPServer) URL() string {
	return m.server.URL
}

func (m *mockHTTPServer) SetStatusCode(code int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.statusCode = code
}

func TestLoadBalancer(t *testing.T) {
	t.Parallel()

	t.Run("empty_backends_error", func(t *testing.T) {
		t.Parallel()

		_, err := NewLoadBalancer(LoadBalancerConfig{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires at least one backend")
	})

	t.Run("round_robin_logic", func(t *testing.T) {
		t.Parallel()
		server1 := newMockHTTPServer(t, http.StatusOK)
		server2 := newMockHTTPServer(t, http.StatusOK)
		server3 := newMockHTTPServer(t, http.StatusOK)

		lb, err := NewLoadBalancer(LoadBalancerConfig{
			Strategy: RoundRobin,
		}, server1.URL(), server2.URL(), server3.URL())
		require.NoError(t, err)
		t.Cleanup(func() { _ = lb.Close() })

		for range 6 {
			req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
			require.NoError(t, err)

			resp, err := lb.Do(req)
			require.NoError(t, err)

			_ = resp.Body.Close()
		}
	})

	t.Run("random_strategy", func(t *testing.T) {
		t.Parallel()
		server1 := newMockHTTPServer(t, http.StatusOK)
		server2 := newMockHTTPServer(t, http.StatusOK)

		lb, err := NewLoadBalancer(LoadBalancerConfig{
			Strategy: Random,
		}, server1.URL(), server2.URL())
		require.NoError(t, err)
		t.Cleanup(func() { _ = lb.Close() })

		for range 10 {
			req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
			require.NoError(t, err)

			resp, err := lb.Do(req)
			require.NoError(t, err)

			_ = resp.Body.Close()
		}
	})

	t.Run("weighted_round_robin", func(t *testing.T) {
		t.Parallel()
		server1 := newMockHTTPServer(t, http.StatusOK)
		server2 := newMockHTTPServer(t, http.StatusOK)

		lb, err := NewLoadBalancer(LoadBalancerConfig{
			Strategy: WeightedRoundRobin,
		}, server1.URL(), server2.URL())
		require.NoError(t, err)
		t.Cleanup(func() { _ = lb.Close() })

		lb.mu.Lock()
		lb.backends[0].Weight = 3
		lb.backends[1].Weight = 1
		lb.mu.Unlock()

		for range 4 {
			req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
			require.NoError(t, err)

			resp, err := lb.Do(req)
			require.NoError(t, err)

			_ = resp.Body.Close()
		}
	})

	t.Run("unhealthy_backend_skipped", func(t *testing.T) {
		t.Parallel()

		server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		t.Cleanup(server1.Close)
		server2 := newMockHTTPServer(t, http.StatusOK)

		lb, err := NewLoadBalancer(LoadBalancerConfig{
			MaxFails:   1,
			RetryAfter: 1 * time.Hour,
		}, server1.URL, server2.URL())
		require.NoError(t, err)
		t.Cleanup(func() { _ = lb.Close() })

		req1, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)
		resp1, err := lb.Do(req1)
		require.NoError(t, err)

		_ = resp1.Body.Close()

		req2, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)
		resp2, err := lb.Do(req2)
		require.NoError(t, err)

		_ = resp2.Body.Close()

		assert.True(t, lb.backends[0].unhealthy.Load(), "server1 should be unhealthy after 1 failure")
	})

	t.Run("health_check_recovers_backend", func(t *testing.T) {
		t.Parallel()
		server1 := newMockHTTPServer(t, http.StatusServiceUnavailable)

		lb, err := NewLoadBalancer(LoadBalancerConfig{
			MaxFails:            1,
			RetryAfter:          1 * time.Hour,
			HealthCheckURL:      server1.URL(),
			HealthCheckInterval: 50 * time.Millisecond,
		}, server1.URL())
		require.NoError(t, err)
		t.Cleanup(func() { _ = lb.Close() })

		server1.SetStatusCode(http.StatusOK)

		time.Sleep(150 * time.Millisecond)

		assert.False(t, lb.backends[0].unhealthy.Load())
	})

	t.Run("stats_set_weight_and_reset", func(t *testing.T) {
		t.Parallel()
		server1 := newMockHTTPServer(t, http.StatusOK)
		server2 := newMockHTTPServer(t, http.StatusOK)

		lb, err := NewLoadBalancer(LoadBalancerConfig{}, server1.URL(), server2.URL())
		require.NoError(t, err)
		t.Cleanup(func() { _ = lb.Close() })

		// Check Stats
		stats := lb.Stats()
		assert.Equal(t, 2, stats.TotalBackends)
		assert.Equal(t, 2, stats.HealthyBackends)
		assert.Equal(t, 0, stats.UnhealthyBackends)

		// Check SetWeight
		ok := lb.SetWeight(server1.URL(), 5)
		assert.True(t, ok)
		assert.Equal(t, 5, lb.backends[0].Weight)

		ok = lb.SetWeight("http://non-existent", 10)
		assert.False(t, ok)

		// Check Reset
		lb.backends[0].MarkFailed()
		lb.backends[0].MarkFailed()
		lb.backends[0].MarkFailed() // MaxFails defaults to 3
		assert.True(t, lb.backends[0].unhealthy.Load())

		assert.Equal(t, 1, lb.Stats().UnhealthyBackends)

		lb.Reset()
		assert.False(t, lb.backends[0].unhealthy.Load())
		assert.Equal(t, 2, lb.Stats().HealthyBackends)
	})

	t.Run("concurrency_safety", func(t *testing.T) {
		t.Parallel()
		server1 := newMockHTTPServer(t, http.StatusOK)
		server2 := newMockHTTPServer(t, http.StatusOK)

		lb, err := NewLoadBalancer(LoadBalancerConfig{}, server1.URL(), server2.URL())
		require.NoError(t, err)
		t.Cleanup(func() { _ = lb.Close() })

		var wg sync.WaitGroup

		iterations := 100
		wg.Add(iterations)

		for range iterations {
			go func() {
				defer wg.Done()

				req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
				if err != nil {
					return
				}

				resp, err := lb.Do(req)
				if err == nil {
					_ = resp.Body.Close()
				}
			}()
		}

		wg.Wait()
	})

	t.Run("update_backends_handling", func(t *testing.T) {
		t.Parallel()

		lb, err := NewLoadBalancer(LoadBalancerConfig{}, "http://backend1")
		require.NoError(t, err)
		t.Cleanup(func() { _ = lb.Close() })

		// Empty update should return early (no modifications)
		statsBefore := lb.Stats()
		lb.UpdateBackends()
		assert.Equal(t, statsBefore, lb.Stats())

		// Update with new backends should replace the list
		lb.UpdateBackends("http://new-backend1", "http://new-backend2")
		assert.Equal(t, 2, lb.Stats().TotalBackends)
		assert.Equal(t, "http://new-backend1", lb.backends[0].URL)
	})

	t.Run("invalid_backend_url_error", func(t *testing.T) {
		t.Parallel()

		lb, err := NewLoadBalancer(LoadBalancerConfig{}, "::invalid_url::")
		require.NoError(t, err)
		t.Cleanup(func() { _ = lb.Close() })

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)

		_, err = lb.Do(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid backend URL")
	})

	t.Run("context_canceled_not_marked_unhealthy", func(t *testing.T) {
		t.Parallel()
		server1 := newMockHTTPServer(t, http.StatusOK)

		lb, err := NewLoadBalancer(LoadBalancerConfig{}, server1.URL())
		require.NoError(t, err)
		t.Cleanup(func() { _ = lb.Close() })

		// Inject mock client that simulates a context canceled error
		mock := DoerFunc(func(req *http.Request) (*http.Response, error) {
			return nil, context.Canceled
		})
		lb.WithClients(mock)

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)

		_, err = lb.Do(req)
		assert.ErrorIs(t, err, context.Canceled)
		assert.False(t, lb.backends[0].unhealthy.Load(), "canceled context should not degrade backend health")
	})

	t.Run("all_backends_unhealthy_fallback_error", func(t *testing.T) {
		t.Parallel()

		lb, err := NewLoadBalancer(LoadBalancerConfig{}, "http://b1")
		require.NoError(t, err)
		t.Cleanup(func() { _ = lb.Close() })

		// Force the single backend to be unhealthy
		lb.backends[0].MarkFailed()
		lb.backends[0].MarkFailed()
		lb.backends[0].MarkFailed()

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)

		_, err = lb.Do(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no healthy backends available")
	})
}

func TestLoadBalancer_Prewarm(t *testing.T) {
	t.Parallel()

	m1 := &loadMockDoer{id: 1}
	m2 := &loadMockDoer{id: 2}

	lb, err := NewLoadBalancer(LoadBalancerConfig{}, "http://backend1", "http://backend2")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lb.Close() })

	lb.WithClients(m1, m2)

	lb.Prewarm(t.Context())

	assert.Equal(t, 1, m1.GetCalls())
	assert.Equal(t, 1, m2.GetCalls())
}
