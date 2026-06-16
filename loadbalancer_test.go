// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadBalancer(t *testing.T) {
	t.Run("Empty backends error", func(t *testing.T) {
		_, err := NewLoadBalancer(LoadBalancerConfig{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires at least one backend")
	})

	t.Run("Round-Robin logic", func(t *testing.T) {
		server1 := newMockHTTPServer(t, http.StatusOK, "server1")
		server2 := newMockHTTPServer(t, http.StatusOK, "server2")
		server3 := newMockHTTPServer(t, http.StatusOK, "server3")

		defer server1.Close()
		defer server2.Close()
		defer server3.Close()

		lb, err := NewLoadBalancer(LoadBalancerConfig{
			Strategy: RoundRobin,
		}, server1.URL(), server2.URL(), server3.URL())
		require.NoError(t, err)

		defer lb.Close()

		// Make 6 requests - each server should get 2
		for range 6 {
			req, _ := http.NewRequest("GET", "http://test", nil)
			resp, err := lb.Do(req)
			require.NoError(t, err)

			_ = resp.Body.Close()
		}
	})

	t.Run("Random strategy", func(t *testing.T) {
		server1 := newMockHTTPServer(t, http.StatusOK, "server1")
		server2 := newMockHTTPServer(t, http.StatusOK, "server2")

		defer server1.Close()
		defer server2.Close()

		lb, err := NewLoadBalancer(LoadBalancerConfig{
			Strategy: Random,
		}, server1.URL(), server2.URL())
		require.NoError(t, err)

		defer lb.Close()

		// Make multiple requests - should not panic
		for range 10 {
			req, _ := http.NewRequest("GET", "http://test", nil)
			resp, err := lb.Do(req)
			require.NoError(t, err)

			_ = resp.Body.Close()
		}
	})

	t.Run("Weighted Round-Robin", func(t *testing.T) {
		server1 := newMockHTTPServer(t, http.StatusOK, "server1")
		server2 := newMockHTTPServer(t, http.StatusOK, "server2")

		defer server1.Close()
		defer server2.Close()

		lb, err := NewLoadBalancer(LoadBalancerConfig{
			Strategy: WeightedRoundRobin,
		}, server1.URL(), server2.URL())
		require.NoError(t, err)

		defer lb.Close()

		// Set weights
		lb.mu.Lock()
		lb.backends[0].Weight = 3
		lb.backends[1].Weight = 1
		lb.mu.Unlock()

		// Make 4 requests
		for range 4 {
			req, _ := http.NewRequest("GET", "http://test", nil)
			resp, err := lb.Do(req)
			require.NoError(t, err)

			_ = resp.Body.Close()
		}
	})

	t.Run("Unhealthy backend skipped", func(t *testing.T) {
		server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		server2 := newMockHTTPServer(t, http.StatusOK, "server2")

		defer server1.Close()
		defer server2.Close()

		lb, err := NewLoadBalancer(LoadBalancerConfig{
			MaxFails:   1,
			RetryAfter: 1 * time.Hour,
		}, server1.URL, server2.URL())
		require.NoError(t, err)

		defer lb.Close()

		// First request: RoundRobin starts at index 1 (server2=OK), succeeds
		req, _ := http.NewRequest("GET", "http://test", nil)
		resp, err := lb.Do(req)
		require.NoError(t, err)

		_ = resp.Body.Close()

		// Second request: RoundRobin picks index 0 (server1=502), fails, then index 1 succeeds
		req, _ = http.NewRequest("GET", "http://test", nil)
		resp, err = lb.Do(req)
		require.NoError(t, err)

		_ = resp.Body.Close()

		// Server1 should be marked unhealthy after 1 failure
		assert.True(t, lb.backends[0].unhealthy.Load(), "server1 should be unhealthy after 1 failure")
	})

	t.Run("Health check recovers backend", func(t *testing.T) {
		server1 := newMockHTTPServer(t, http.StatusServiceUnavailable, "server1")
		defer server1.Close()

		lb, err := NewLoadBalancer(LoadBalancerConfig{
			MaxFails:            1,
			RetryAfter:          1 * time.Hour,
			HealthCheckURL:      server1.URL(),
			HealthCheckInterval: 50 * time.Millisecond,
		}, server1.URL())
		require.NoError(t, err)

		defer lb.Close()

		// Make server healthy
		server1.SetStatusCode(http.StatusOK)

		// Wait for health check to run
		time.Sleep(150 * time.Millisecond)

		// Backend should be healthy now
		assert.False(t, lb.backends[0].unhealthy.Load())
	})

	t.Run("Concurrency safety", func(t *testing.T) {
		server1 := newMockHTTPServer(t, http.StatusOK, "server1")
		server2 := newMockHTTPServer(t, http.StatusOK, "server2")

		defer server1.Close()
		defer server2.Close()

		lb, err := NewLoadBalancer(LoadBalancerConfig{}, server1.URL(), server2.URL())
		require.NoError(t, err)

		defer lb.Close()

		var wg sync.WaitGroup

		iterations := 100
		wg.Add(iterations)

		for range iterations {
			go func() {
				defer wg.Done()

				req, _ := http.NewRequest("GET", "http://test", nil)

				resp, err := lb.Do(req)
				if err == nil {
					_ = resp.Body.Close()
				}
			}()
		}

		wg.Wait()
	})
}

type mockHTTPServer struct {
	server     *httptest.Server
	statusCode int
	mu         sync.RWMutex
}

func newMockHTTPServer(t *testing.T, statusCode int, _ string) *mockHTTPServer {
	t.Helper()

	m := &mockHTTPServer{statusCode: statusCode}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		code := m.statusCode
		m.mu.RUnlock()
		w.WriteHeader(code)
	}))

	return m
}

func (m *mockHTTPServer) Close() {
	m.server.Close()
}

func (m *mockHTTPServer) URL() string {
	return m.server.URL
}

func (m *mockHTTPServer) SetStatusCode(code int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.statusCode = code
}

func TestLoadBalancer_Prewarm(t *testing.T) {
	m1 := &mockDoer{id: 1}
	m2 := &mockDoer{id: 2}

	lb, err := NewLoadBalancer(LoadBalancerConfig{}, "http://backend1", "http://backend2")
	require.NoError(t, err)

	defer lb.Close()

	lb.WithClients(m1, m2)

	ctx := context.Background()
	lb.Prewarm(ctx)

	assert.Equal(t, 1, m1.GetCalls())
	assert.Equal(t, 1, m2.GetCalls())
}
