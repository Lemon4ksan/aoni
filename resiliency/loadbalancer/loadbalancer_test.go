// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package loadbalancer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/health"
)

func TestLoadBalancer_Initialization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		backends    []string
		cfg         Config
		expectedErr error
	}{
		{
			name:        "empty_backends_returns_error",
			backends:    nil,
			cfg:         Config{},
			expectedErr: ErrNoBackends,
		},
		{
			name:        "single_backend_success",
			backends:    []string{"http://127.0.0.1:8080"},
			cfg:         Config{},
			expectedErr: nil,
		},
		{
			name:        "defaults_applied_correctly",
			backends:    []string{"http://127.0.0.1:8080"},
			cfg:         Config{MaxFails: 0, RetryAfter: 0},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lb, err := New(tt.cfg, tt.backends...)
			if tt.expectedErr != nil {
				require.ErrorIs(t, err, tt.expectedErr)
				assert.Nil(t, lb)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, lb)
			t.Cleanup(func() { _ = lb.Close() })

			assert.Equal(t, uint32(3), lb.config.MaxFails)
			assert.Equal(t, 30*time.Second, lb.config.RetryAfter)
		})
	}
}

func TestLoadBalancer_Strategies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		strategy Strategy
		requests int
	}{
		{
			name:     "round_robin_distribution",
			strategy: RoundRobin,
			requests: 6,
		},
		{
			name:     "random_distribution",
			strategy: Random,
			requests: 10,
		},
		{
			name:     "weighted_round_robin_distribution",
			strategy: WeightedRoundRobin,
			requests: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(s1.Close)

			s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(s2.Close)

			lb, err := New(Config{Strategy: tt.strategy}, s1.URL, s2.URL)
			require.NoError(t, err)
			t.Cleanup(func() { _ = lb.Close() })

			if tt.strategy == WeightedRoundRobin {
				lb.SetWeight(s1.URL, 3)
				lb.SetWeight(s2.URL, 1)
			}

			for range tt.requests {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://test", nil)
				require.NoError(t, err)

				resp, err := lb.Do(req)
				require.NoError(t, err)

				_ = resp.Body.Close()
			}
		})
	}
}

func TestLoadBalancer_FaultHandling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		handler        http.HandlerFunc
		doerErr        error
		expectFault    bool
		expectErrIs    error
		checkUnhealthy bool
	}{
		{
			name: "success_200_ok",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			expectFault: false,
		},
		{
			name: "application_error_404_not_fault",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectFault: false,
		},
		{
			name: "gateway_fault_502_bad_gateway",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
			expectFault:    true,
			checkUnhealthy: true,
		},
		{
			name: "gateway_fault_503_service_unavailable",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			},
			expectFault:    true,
			checkUnhealthy: true,
		},
		{
			name: "gateway_fault_504_gateway_timeout",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusGatewayTimeout)
			},
			expectFault:    true,
			checkUnhealthy: true,
		},
		{
			name:        "context_canceled_does_not_degrade_health",
			doerErr:     context.Canceled,
			expectFault: false,
			expectErrIs: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.handler != nil {
					tt.handler(w, r)
				}
			}))
			t.Cleanup(server.Close)

			lb, err := New(Config{
				MaxFails:   1,
				RetryAfter: 1 * time.Hour,
			}, server.URL)
			require.NoError(t, err)
			t.Cleanup(func() { _ = lb.Close() })

			if tt.doerErr != nil {
				lb.WithClients(aoni.HTTPDoerFunc(func(_ *http.Request) (*http.Response, error) {
					return nil, tt.doerErr
				}))
			}

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://test", nil)
			require.NoError(t, err)

			resp, err := lb.Do(req)
			if tt.expectErrIs != nil {
				assert.ErrorIs(t, err, tt.expectErrIs)
			}

			if resp != nil {
				_ = resp.Body.Close()
			}

			if tt.checkUnhealthy {
				assert.False(t, lb.backends[0].IsAvailable())
			} else {
				assert.True(t, lb.backends[0].IsAvailable())
			}
		})
	}
}

func TestLoadBalancer_BackendManagement(t *testing.T) {
	t.Parallel()

	t.Run("set_weight_and_stats", func(t *testing.T) {
		t.Parallel()

		lb, err := New(Config{}, "http://b1", "http://b2")
		require.NoError(t, err)
		t.Cleanup(func() { _ = lb.Close() })

		stats := lb.Stats()
		assert.Equal(t, 2, stats.TotalBackends)
		assert.Equal(t, 2, stats.HealthyBackends)
		assert.Equal(t, 0, stats.UnhealthyBackends)

		assert.True(t, lb.SetWeight("http://b1", 10))
		assert.Equal(t, 10, lb.backends[0].Weight)
		assert.False(t, lb.SetWeight("http://unknown", 5))
	})

	t.Run("update_backends", func(t *testing.T) {
		t.Parallel()

		lb, err := New(Config{}, "http://b1")
		require.NoError(t, err)
		t.Cleanup(func() { _ = lb.Close() })

		lb.UpdateBackends()
		assert.Equal(t, 1, lb.Stats().TotalBackends)

		lb.UpdateBackends("http://new1", "http://new2")
		assert.Equal(t, 2, lb.Stats().TotalBackends)
		assert.Equal(t, "http://new1", lb.backends[0].URL)
	})

	t.Run("backend_accessors", func(t *testing.T) {
		t.Parallel()

		lb, err := New(Config{MaxFails: 1}, "http://b1")
		require.NoError(t, err)
		t.Cleanup(func() { _ = lb.Close() })

		b := lb.backends[0]
		assert.True(t, b.IsAvailable())
		assert.Equal(t, health.StatusHealthy, b.Status())
		assert.Equal(t, uint32(0), b.FailCount())

		b.tracker.MarkFailed()

		assert.False(t, b.IsAvailable())
		assert.Equal(t, health.StatusUnhealthy, b.Status())
		assert.Equal(t, uint32(1), b.FailCount())

		lb.Reset()
		assert.True(t, b.IsAvailable())
		assert.Equal(t, health.StatusHealthy, b.Status())
	})
}

func TestLoadBalancer_HealthCheckRecovery(t *testing.T) {
	t.Parallel()

	var statusCode atomic.Int32
	statusCode.Store(http.StatusServiceUnavailable)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(statusCode.Load()))
	}))
	t.Cleanup(server.Close)

	lb, err := New(Config{
		MaxFails:            1,
		RetryAfter:          1 * time.Hour,
		HealthCheckURL:      server.URL,
		HealthCheckInterval: 20 * time.Millisecond,
	}, server.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lb.Close() })

	lb.backends[0].tracker.MarkFailed()
	assert.False(t, lb.backends[0].IsAvailable())

	statusCode.Store(http.StatusOK)

	assert.Eventually(t, func() bool {
		return lb.backends[0].IsAvailable()
	}, 1*time.Second, 10*time.Millisecond)
}

func TestLoadBalancer_Prewarm(t *testing.T) {
	t.Parallel()

	var calls1, calls2 atomic.Int32

	c1 := aoni.HTTPDoerFunc(func(req *http.Request) (*http.Response, error) {
		calls1.Add(1)
		assert.Equal(t, http.MethodHead, req.Method)

		return &http.Response{StatusCode: http.StatusOK}, nil
	})

	c2 := aoni.HTTPDoerFunc(func(req *http.Request) (*http.Response, error) {
		calls2.Add(1)
		assert.Equal(t, http.MethodHead, req.Method)

		return &http.Response{StatusCode: http.StatusOK}, nil
	})

	lb, err := New(Config{}, "http://b1", "http://b2")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lb.Close() })

	lb.WithClients(c1, c2)
	lb.Prewarm(t.Context())

	assert.Equal(t, int32(1), calls1.Load())
	assert.Equal(t, int32(1), calls2.Load())
}

func TestLoadBalancer_AllBackendsFailed(t *testing.T) {
	t.Parallel()

	lb, err := New(Config{MaxFails: 1}, "http://b1", "http://b2")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lb.Close() })

	for _, b := range lb.backends {
		b.tracker.MarkFailed()
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://test", nil)
	require.NoError(t, err)

	_, err = lb.Do(req)
	assert.ErrorIs(t, err, ErrNoHealthyBackends)
}

func TestLoadBalancer_InvalidBackendURL(t *testing.T) {
	t.Parallel()

	lb, err := New(Config{}, "::invalid_url::")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lb.Close() })

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://test", nil)
	require.NoError(t, err)

	_, err = lb.Do(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "all backends failed")
}

func TestLoadBalancer_SRVError(t *testing.T) {
	t.Parallel()

	_, err := NewSRV(t.Context(), "invalid_service", "tcp", "invalid_domain.local", "http", 0, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "srv loadbalancer")
}

func TestLoadBalancer_SelectHealthy_And_DoResult(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	lb, err := New(Config{}, server.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lb.Close() })

	opt := lb.SelectHealthy()
	require.True(t, opt.IsPresent())
	be, ok := opt.Value()
	require.True(t, ok)
	assert.Equal(t, server.URL, be.URL)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://test", nil)
	require.NoError(t, err)

	res := lb.DoResult(req)
	require.True(t, res.IsSuccess())
	resp, err := res.Unwrap()
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
