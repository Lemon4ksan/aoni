// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// LoadBalancingStrategy defines the selection algorithm for backends.
type LoadBalancingStrategy int

const (
	// RoundRobin selects backends sequentially in a cyclic order.
	RoundRobin LoadBalancingStrategy = iota
	// Random selects backends randomly for each request.
	Random
	// WeightedRoundRobin selects backends based on their assigned weights.
	WeightedRoundRobin
)

// LoadBalancerConfig defines the tuning and policy settings for a [LoadBalancer].
type LoadBalancerConfig struct {
	// Strategy determines the selection algorithm for choosing a backend.
	Strategy LoadBalancingStrategy
	// MaxFails is the consecutive failure threshold before a backend is marked unhealthy.
	MaxFails uint32
	// RetryAfter is the offline cooldown duration for an unhealthy backend.
	RetryAfter time.Duration
	// HealthCheckURL is the server endpoint probed by background health checks.
	HealthCheckURL string
	// HealthCheckInterval is the period between sequential background health checks.
	HealthCheckInterval time.Duration
}

// Backend tracks the health and connection state of a single server in the load balancer.
type Backend struct {
	// URL is the address of the backend server.
	URL string

	// Weight is the selection weight for the [WeightedRoundRobin] strategy.
	Weight int

	client      HTTPDoer
	failCount   atomic.Uint32
	unhealthy   atomic.Bool
	recoveredAt atomic.Int64
}

// LoadBalancer distributes requests across multiple backend servers.
// It implements the [HTTPDoer] interface and can be passed to [NewClient].
// Use [NewLoadBalancer] to initialize new instances.
type LoadBalancer struct {
	mu       sync.RWMutex
	backends []*Backend
	config   LoadBalancerConfig
	current  atomic.Uint64

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewLoadBalancer initializes a new [LoadBalancer] with the given backends.
// It returns an error if the backends slice is empty.
// If MaxFails or RetryAfter configuration options are zero, they default to 3 and 30 seconds respectively.
func NewLoadBalancer(cfg LoadBalancerConfig, backends ...string) (*LoadBalancer, error) {
	if len(backends) == 0 {
		return nil, errors.New("aoni: load balancer requires at least one backend")
	}

	if cfg.MaxFails == 0 {
		cfg.MaxFails = 3
	}

	if cfg.RetryAfter == 0 {
		cfg.RetryAfter = 30 * time.Second
	}

	tracked := make([]*Backend, len(backends))
	for i, u := range backends {
		tracked[i] = &Backend{
			URL:    u,
			Weight: 1,
			client: &http.Client{Timeout: 15 * time.Second},
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	lb := &LoadBalancer{
		ctx:      ctx,
		cancel:   cancel,
		backends: tracked,
		config:   cfg,
	}

	lb.wg.Go(lb.healthCheckLoop)

	return lb, nil
}

// WithClients applies custom [HTTPDoer] clients to the corresponding backends.
// The clients are matched to backends by slice index order.
// If the number of clients is different from backends, any excess clients are ignored.
func (lb *LoadBalancer) WithClients(clients ...HTTPDoer) *LoadBalancer {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for i, c := range clients {
		if i < len(lb.backends) {
			lb.backends[i].client = c
		}
	}

	return lb
}

// UpdateBackends replaces the entire set of active backends.
// It resets the selection counter to zero.
// If the backends slice is empty, the method returns early and makes no changes.
func (lb *LoadBalancer) UpdateBackends(backends ...string) {
	if len(backends) == 0 {
		return
	}

	tracked := make([]*Backend, len(backends))
	for i, u := range backends {
		tracked[i] = &Backend{
			URL:    u,
			Weight: 1,
			client: &http.Client{Timeout: 15 * time.Second},
		}
	}

	lb.mu.Lock()
	lb.backends = tracked
	lb.current.Store(0)
	lb.mu.Unlock()
}

// Close stops background health check routines.
func (lb *LoadBalancer) Close() error {
	lb.cancel()
	lb.wg.Wait()

	for _, tc := range lb.backends {
		if httpClient, ok := tc.client.(*http.Client); ok {
			if transport, ok := httpClient.Transport.(*http.Transport); ok {
				transport.CloseIdleConnections()
			}
		}
	}

	return nil
}

// Do executes the HTTP request across healthy backends based on the configured strategy.
// It clones the original request and overwrites its Scheme and Host to target the chosen backend.
//
// If the chosen backend fails health checks, it is marked as failed.
// The load balancer then retries other available backends sequentially.
// If all backends fail, it returns an error wrapping the last encountered failure.
func (lb *LoadBalancer) Do(req *http.Request) (*http.Response, error) {
	lb.mu.RLock()
	backends := lb.backends
	lb.mu.RUnlock()

	var lastErr error

	n := uint64(len(backends))

	// Build iteration order: shuffled indices for Random, sequential for others.
	indices := make([]uint64, n)
	for i := range indices {
		indices[i] = uint64(i)
	}

	switch lb.config.Strategy {
	case Random:
		rand.Shuffle(len(indices), func(i, j int) {
			indices[i], indices[j] = indices[j], indices[i]
		})
	case WeightedRoundRobin:
		// Sort indices by weight (descending) for weighted selection.
		slices.SortStableFunc(indices, func(a, b uint64) int {
			return backends[b].Weight - backends[a].Weight
		})
	default:
		// RoundRobin: start from current position and wrap around.
		start := lb.current.Add(1) % n
		for i := range indices {
			indices[i] = (start + uint64(i)) % n
		}
	}

	for _, idx := range indices {
		b := backends[idx]

		if !lb.isAvailable(b) {
			continue
		}

		backendURL, err := url.Parse(b.URL)
		if err != nil {
			lastErr = fmt.Errorf("invalid backend URL %q: %w", b.URL, err)
			continue
		}

		backendReq := req.Clone(req.Context())
		backendReq.URL.Scheme = backendURL.Scheme
		backendReq.URL.Host = backendURL.Host

		resp, err := b.client.Do(backendReq)
		if lb.isFault(resp, err) {
			lb.markFailed(b)

			lastErr = err

			if resp != nil {
				_ = resp.Body.Close()
			}

			continue
		}

		lb.markSuccess(b)

		return resp, err
	}

	if lastErr != nil {
		return nil, fmt.Errorf("aoni: all backends failed, last error: %w", lastErr)
	}

	return nil, errors.New("aoni: no healthy backends available")
}

func (lb *LoadBalancer) isAvailable(b *Backend) bool {
	if !b.unhealthy.Load() {
		return true
	}

	if time.Now().UnixNano() >= b.recoveredAt.Load() {
		return true
	}

	return false
}

func (lb *LoadBalancer) markFailed(b *Backend) {
	fails := b.failCount.Add(1)
	if fails >= lb.config.MaxFails {
		b.unhealthy.Store(true)

		recoveryTime := time.Now().Add(lb.config.RetryAfter).UnixNano()
		b.recoveredAt.Store(recoveryTime)

		slog.Warn("backend marked unhealthy", //nolint:gosec
			"backend", b.URL,
			"fails", fails,
			"retry_after", lb.config.RetryAfter,
		)
	}
}

func (lb *LoadBalancer) markSuccess(b *Backend) {
	wasUnhealthy := b.unhealthy.Load()
	b.failCount.Store(0)
	b.unhealthy.Store(false)

	if wasUnhealthy {
		slog.Info("backend recovered", //nolint:gosec
			"backend", b.URL,
		)
	}
}

func (lb *LoadBalancer) isFault(resp *http.Response, err error) bool {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return false
		}

		var netErr net.Error
		if errors.As(err, &netErr) {
			return true
		}

		return true
	}

	if resp != nil {
		if resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusGatewayTimeout ||
			resp.StatusCode == http.StatusServiceUnavailable {
			return true
		}
	}

	return false
}

func (lb *LoadBalancer) healthCheckLoop() {
	if lb.config.HealthCheckURL == "" {
		return
	}

	if lb.config.HealthCheckInterval == 0 {
		lb.config.HealthCheckInterval = 1 * time.Minute
	}

	ticker := time.NewTicker(lb.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-lb.ctx.Done():
			return
		case <-ticker.C:
			lb.mu.RLock()
			backends := lb.backends
			lb.mu.RUnlock()

			for _, b := range backends {
				if b.unhealthy.Load() {
					lb.checkHealth(b)
				}
			}
		}
	}
}

func (lb *LoadBalancer) checkHealth(b *Backend) {
	req, err := http.NewRequestWithContext(lb.ctx, http.MethodGet, lb.config.HealthCheckURL, nil)
	if err != nil {
		return
	}

	resp, err := b.client.Do(req)
	if err == nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			lb.markSuccess(b)
		}

		_ = resp.Body.Close()
	}
}

// Prewarm opens TCP/TLS connections to all registered backends by executing
// concurrent HEAD requests to each backend URL.
func (lb *LoadBalancer) Prewarm(ctx context.Context) {
	lb.mu.RLock()
	backends := make([]*Backend, len(lb.backends))
	copy(backends, lb.backends)
	lb.mu.RUnlock()

	var wg sync.WaitGroup
	for _, b := range backends {
		wg.Add(1)

		go func(backend *Backend) {
			defer wg.Done()

			req, err := http.NewRequestWithContext(ctx, http.MethodHead, backend.URL, nil)
			if err != nil {
				return
			}

			resp, err := backend.client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
			}
		}(b)
	}

	wg.Wait()
}
