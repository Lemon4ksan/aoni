// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/resiliency/loadbalancer"
)

func TestFast_Resiliency_RetryAfterBackoff(t *testing.T) {
	var attempts atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, "rate limited")

			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "rate limit recovered ok")
	}))
	defer ts.Close()

	fastClient := fast.NewClient()

	chained := middleware.Chain(
		fastClient,
		middleware.Retry(
			middleware.RetryOptions{
				MaxRetries: 3,
				Backoff:    10 * time.Millisecond,
			},
			middleware.RetryOnRateLimit(),
		),
	)

	req := fast.NewRequest(nil)
	req.SetURL(ts.URL)
	req.SetMethod("GET")

	resp, err := chained.Do(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, "rate limit recovered ok", string(resp.BodyBytes()))
	assert.Equal(t, int32(2), attempts.Load())
}

func TestFast_Resiliency_CircuitBreaker(t *testing.T) {
	var serverHits atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	cb := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
		Cooldown:         5 * time.Second,
		MinRequests:      2,
		FailureThreshold: 0.5,
	})

	fastClient := fast.NewClient()
	cbClient := middleware.CircuitBreak(cb, nil)(fastClient)

	// Request 1 -> 500 Server Error
	req1 := fast.NewRequest(nil)
	req1.SetURL(ts.URL)
	req1.SetMethod("GET")

	resp1, _ := cbClient.Do(req1)
	if resp1 != nil {
		resp1.Close()
	}

	// Request 2 -> 500 Server Error (Trips Circuit Breaker)
	req2 := fast.NewRequest(nil)
	req2.SetURL(ts.URL)
	req2.SetMethod("GET")

	resp2, _ := cbClient.Do(req2)
	if resp2 != nil {
		resp2.Close()
	}

	// Request 3 -> Should fail immediately with circuit open error without hitting server
	hitsBefore := serverHits.Load()
	req3 := fast.NewRequest(nil)
	req3.SetURL(ts.URL)
	req3.SetMethod("GET")

	resp3, err3 := cbClient.Do(req3)
	if resp3 != nil {
		resp3.Close()
	}

	assert.Error(t, err3)
	assert.Contains(t, err3.Error(), "circuit breaker open")
	assert.Equal(t, hitsBefore, serverHits.Load())
}

func TestFast_Resiliency_LoadBalancer(t *testing.T) {
	s1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "server 1")
	}))
	defer s1.Close()

	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "server 2")
	}))
	defer s2.Close()

	c1 := fast.NewStdClient(fast.NewClient())
	c2 := fast.NewStdClient(fast.NewClient())

	balancer, err := loadbalancer.New(loadbalancer.Config{
		Strategy: loadbalancer.RoundRobin,
	}, s1.URL, s2.URL)
	require.NoError(t, err)

	balancer.WithClients(c1, c2)

	bodies := make(map[string]int)
	for range 4 {
		req, reqErr := http.NewRequestWithContext(context.Background(), "GET", s1.URL, nil)
		require.NoError(t, reqErr)

		resp, doErr := balancer.Do(req)
		require.NoError(t, doErr)

		bodies[resp.Header.Get("X-Server-Id")]++
		_ = resp.Body.Close()
	}

	assert.NoError(t, balancer.Close())
}
