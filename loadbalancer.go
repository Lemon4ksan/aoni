// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// LoadBalancingStrategy defines the strategy for selecting backends.
type LoadBalancingStrategy int

const (
	// RoundRobin distributes requests sequentially across backends.
	RoundRobin LoadBalancingStrategy = iota
	// Random selects a random backend for each request.
	Random
	// WeightedRoundRobin distributes requests based on backend weights.
	WeightedRoundRobin
)

// LoadBalancerConfig defines the configuration for a LoadBalancer.
type LoadBalancerConfig struct {
	// Strategy defines the load balancing strategy.
	Strategy LoadBalancingStrategy
	// MaxFails is the number of sequential errors allowed before a backend is marked unhealthy.
	MaxFails uint32
	// RetryAfter is the duration for which an unhealthy backend is excluded from rotation.
	RetryAfter time.Duration
	// HealthCheckURL is the endpoint used for background health checks.
	HealthCheckURL string
	// HealthCheckInterval is the interval at which background health checks run.
	HealthCheckInterval time.Duration
}

// Backend represents a single backend server in the load balancer.
type Backend struct {
	// URL is the backend server URL.
	URL string
	// Weight is the weight for weighted round-robin (default: 1).
	Weight int
	// client is the underlying HTTP client for this backend.
	client HTTPDoer
	// failCount tracks consecutive failures.
	failCount atomic.Uint32
	// unhealthy indicates if the backend is currently unhealthy.
	unhealthy atomic.Bool
	// recoveredAt is the timestamp when the backend becomes available again.
	recoveredAt atomic.Int64
}

// LoadBalancer distributes requests across multiple backends.
// It implements the [HTTPDoer] interface and can be passed to [NewClient].
//
// Create new instances of LoadBalancer using [NewLoadBalancer].
type LoadBalancer struct {
	mu       sync.RWMutex
	backends []*Backend
	config   LoadBalancerConfig
	current  atomic.Uint64

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewLoadBalancer initializes a new LoadBalancer.
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

// WithClients sets custom HTTPDoer instances for each backend.
// The order must match the order of backends passed to NewLoadBalancer.
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

// UpdateBackends replaces the current set of backends with a new one.
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

// Close stops background health checks.
func (lb *LoadBalancer) Close() error {
	lb.cancel()
	lb.wg.Wait()

	return nil
}

// Do performs an HTTP request using the selected backend.
func (lb *LoadBalancer) Do(req *http.Request) (*http.Response, error) {
	lb.mu.RLock()
	backends := lb.backends
	lb.mu.RUnlock()

	var lastErr error

	n := uint64(len(backends))

	for range n {
		idx := lb.nextIndex(n)
		b := backends[idx]

		if !lb.isAvailable(b) {
			continue
		}

		// Rewrite the request URL to point to the backend
		backendURL, err := url.Parse(b.URL)
		if err != nil {
			lastErr = fmt.Errorf("invalid backend URL %q: %w", b.URL, err)
			continue
		}

		// Clone the request to avoid modifying the original
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

func (lb *LoadBalancer) nextIndex(n uint64) uint64 {
	switch lb.config.Strategy {
	case Random:
		return rand.Uint64() % n //nolint:gosec
	case WeightedRoundRobin:
		return lb.weightedIndex(n)
	default:
		return lb.current.Add(1) % n
	}
}

func (lb *LoadBalancer) weightedIndex(n uint64) uint64 {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	totalWeight := 0
	for _, b := range lb.backends {
		totalWeight += b.Weight
	}

	if totalWeight == 0 {
		return lb.current.Add(1) % n
	}

	target := rand.IntN(totalWeight) //nolint:gosec
	cumulative := 0

	for i, b := range lb.backends {
		cumulative += b.Weight
		if target < cumulative {
			return uint64(i)
		}
	}

	return 0
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
	}
}

func (lb *LoadBalancer) markSuccess(b *Backend) {
	b.failCount.Store(0)
	b.unhealthy.Store(false)
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

// Prewarm establishes TCP/TLS connections to all backends in the load balancer.
// It sends a fast parallel HEAD request to each backend's URL to pre-populate the connection pool.
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
