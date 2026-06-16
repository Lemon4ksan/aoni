// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
}

func TestLoadBalancer_Prewarm(t *testing.T) {
	t.Parallel()
	m1 := &mockDoer{id: 1}
	m2 := &mockDoer{id: 2}

	lb, err := NewLoadBalancer(LoadBalancerConfig{}, "http://backend1", "http://backend2")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lb.Close() })

	lb.WithClients(m1, m2)

	lb.Prewarm(t.Context())

	assert.Equal(t, 1, m1.GetCalls())
	assert.Equal(t, 1, m2.GetCalls())
}
