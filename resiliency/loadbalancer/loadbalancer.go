// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package loadbalancer implements client-side HTTP load balancing across multiple target servers.
package loadbalancer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/miyako/generic"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/health"
	fastrand "github.com/lemon4ksan/aoni/internal/rand"
)

var (
	// ErrNoBackends is returned when attempting to initialize a load balancer without target backends.
	ErrNoBackends = errors.New("aoni: load balancer requires at least one backend")

	// ErrNoHealthyBackends is returned when all registered backends are marked unhealthy or in cooldown.
	ErrNoHealthyBackends = errors.New("aoni: no healthy backends available")
)

// Strategy defines the backend selection algorithm.
type Strategy int

const (
	// RoundRobin selects backends sequentially in cyclic order.
	RoundRobin Strategy = iota
	// Random selects backends pseudo-randomly for each request attempt.
	Random
	// WeightedRoundRobin selects backends according to assigned relative weights.
	WeightedRoundRobin
)

// Stats holds health metrics for the backend pool.
type Stats struct {
	TotalBackends     int
	HealthyBackends   int
	UnhealthyBackends int
}

// Config tunes selection algorithms and background health-checking behavior.
type Config struct {
	Strategy            Strategy
	MaxFails            uint32
	RetryAfter          time.Duration
	HealthCheckURL      string
	HealthCheckInterval time.Duration
}

// Backend tracks the health state, weight, and client instance for a single target server.
type Backend struct {
	URL    string
	Weight int

	client    aoni.HTTPDoer
	tracker   *health.Tracker
	parsedURL *url.URL
	parseErr  error
}

// IsAvailable reports whether the backend is currently operational and available for requests.
func (b *Backend) IsAvailable() bool {
	return b.tracker.IsAvailable()
}

// Status returns the current health status of the backend.
func (b *Backend) Status() health.Status {
	return b.tracker.Status()
}

// FailCount returns the consecutive recorded failure count for the backend.
func (b *Backend) FailCount() uint32 {
	return b.tracker.FailCount()
}

// Balancer distributes outgoing HTTP requests across a pool of target backends.
// Implements [HTTPDoer] and supports failover, weighted selection, and automated background probing.
type Balancer struct {
	mu       sync.RWMutex
	backends []*Backend
	config   Config
	current  atomic.Uint64

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New instantiates a [Balancer] targeting the specified backend URLs.
// Returns [ErrNoBackends] if backends is empty.
func New(cfg Config, backends ...string) (*Balancer, error) {
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}

	cfg.MaxFails = generic.Coalesce(cfg.MaxFails, 3)
	cfg.RetryAfter = generic.Coalesce(cfg.RetryAfter, 30*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	lb := &Balancer{
		ctx:      ctx,
		cancel:   cancel,
		backends: createBackends(backends, cfg),
		config:   cfg,
	}

	lb.wg.Go(lb.healthCheckLoop)

	return lb, nil
}

// WithClients binds custom [HTTPDoer] clients to backends by slice index order.
func (b *Balancer) WithClients(clients ...aoni.HTTPDoer) *Balancer {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, c := range clients {
		if i < len(b.backends) && c != nil {
			b.backends[i].client = c
		}
	}

	return b
}

// NewSRV creates a new [Balancer] that dynamically discovers and balances requests across
// backends resolved from DNS SRV records (e.g. service="_http", proto="_tcp", name="service.consul").
func NewSRV(
	ctx context.Context,
	service, proto, name, scheme string,
	refreshInterval time.Duration,
	clientFactory func(targetURL string) aoni.HTTPDoer,
) (*Balancer, error) {
	return NewSRVWeightedRoundRobin(ctx, service, proto, name, scheme, refreshInterval, clientFactory)
}

// NewSRVWeightedRoundRobin initializes a Weighted Round-Robin load balancer populated from DNS SRV records.
func NewSRVWeightedRoundRobin(
	ctx context.Context,
	service, proto, name, scheme string,
	refreshInterval time.Duration,
	clientFactory func(targetURL string) aoni.HTTPDoer,
) (*Balancer, error) {
	backends, err := resolveSRVBackends(service, proto, name, scheme, clientFactory)
	if err != nil {
		return nil, fmt.Errorf("aoni: srv loadbalancer: initial DNS SRV lookup failed: %w", err)
	}

	cfg := Config{
		Strategy:            WeightedRoundRobin,
		MaxFails:            3,
		RetryAfter:          30 * time.Second,
		HealthCheckInterval: refreshInterval,
	}

	childCtx, cancel := context.WithCancel(ctx)
	lb := &Balancer{
		ctx:      childCtx,
		cancel:   cancel,
		backends: backends,
		config:   cfg,
	}

	if refreshInterval > 0 {
		lb.wg.Go(func() {
			ticker := time.NewTicker(refreshInterval)
			defer ticker.Stop()

			for {
				select {
				case <-lb.ctx.Done():
					return
				case <-ticker.C:
					lb.refreshSRV(service, proto, name, scheme, clientFactory)
				}
			}
		})
	}

	return lb, nil
}

func resolveSRVBackends(
	service, proto, name, scheme string,
	clientFactory func(targetURL string) aoni.HTTPDoer,
) ([]*Backend, error) {
	_, records, err := net.LookupSRV(service, proto, name) //nolint:noctx
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, ErrNoBackends
	}

	cfg := Config{MaxFails: 3, RetryAfter: 30 * time.Second}

	backends := make([]*Backend, 0, len(records))
	for _, rec := range records {
		targetHost := strings.TrimSuffix(rec.Target, ".")
		targetURL := fmt.Sprintf("%s://%s:%d", scheme, targetHost, rec.Port)

		parsed, parseErr := url.Parse(targetURL)

		w := int(rec.Weight)
		if w <= 0 {
			w = 1
		}

		var doer aoni.HTTPDoer
		if clientFactory != nil {
			doer = clientFactory(targetURL)
		} else {
			doer = http.DefaultClient
		}

		backends = append(backends, &Backend{
			URL:    targetURL,
			Weight: w,
			client: doer,
			tracker: health.NewTracker(
				targetURL, cfg.MaxFails, cfg.RetryAfter,
				func(name string, fails uint32, retryAfter time.Duration) {
					slog.Warn(
						"srv backend marked unhealthy",
						"backend",
						name,
						"fails",
						fails,
						"retry_after",
						retryAfter,
					)
				},
				func(name string) {
					slog.Info("srv backend recovered", "backend", name)
				},
			),
			parsedURL: parsed,
			parseErr:  parseErr,
		})
	}

	return backends, nil
}

func (b *Balancer) refreshSRV(
	service, proto, name, scheme string,
	clientFactory func(targetURL string) aoni.HTTPDoer,
) {
	newBackends, err := resolveSRVBackends(service, proto, name, scheme, clientFactory)
	if err != nil || len(newBackends) == 0 {
		return
	}

	b.SetBackendPool(newBackends)
}

// SetBackendPool dynamically replaces the active backend pool.
func (b *Balancer) SetBackendPool(backends []*Backend) {
	if len(backends) == 0 {
		return
	}

	b.mu.Lock()
	b.backends = backends
	b.current.Store(0)
	b.mu.Unlock()
}

// UpdateBackends dynamically replaces the active backend pool from string URLs and resets selection counters.
func (b *Balancer) UpdateBackends(backends ...string) {
	if len(backends) == 0 {
		return
	}

	tracked := createBackends(backends, b.config)
	b.SetBackendPool(tracked)
}

// Close stops background health check workers and closes idle backend keep-alive connections.
func (b *Balancer) Close() error {
	b.cancel()
	b.wg.Wait()

	b.mu.RLock()
	backends := b.backends
	b.mu.RUnlock()

	for _, b := range backends {
		if httpClient, ok := b.client.(*http.Client); ok {
			if transport, ok := httpClient.Transport.(*http.Transport); ok {
				transport.CloseIdleConnections()
			}
		}
	}

	return nil
}

// Do executes req against a healthy backend according to the configured strategy.
// Automatically retries remaining backends if a backend experiences connection faults.
func (b *Balancer) Do(req *http.Request) (*http.Response, error) {
	b.mu.RLock()
	backends := b.backends
	b.mu.RUnlock()

	n := uint64(len(backends))
	if n == 0 {
		return nil, ErrNoHealthyBackends
	}

	indices := b.buildBackendIndices(n, backends)

	var lastErr error

	for _, idx := range indices {
		backend := backends[idx]
		if !backend.tracker.IsAvailable() {
			continue
		}

		if backend.parseErr != nil {
			backend.tracker.MarkFailed()
			lastErr = fmt.Errorf("invalid backend URL %q: %w", backend.URL, backend.parseErr)
			continue
		}

		backendReq := req.Clone(req.Context())
		if backend.parsedURL != nil {
			backendReq.URL.Scheme = backend.parsedURL.Scheme
			backendReq.URL.Host = backend.parsedURL.Host
		}

		resp, err := backend.client.Do(backendReq)
		if b.isFault(resp, err) {
			backend.tracker.MarkFailed()

			lastErr = err

			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}

			continue
		}

		backend.tracker.MarkSuccess()

		return resp, err
	}

	if lastErr != nil {
		return nil, fmt.Errorf("aoni: all backends failed, last error: %w", lastErr)
	}

	return nil, ErrNoHealthyBackends
}

func (b *Balancer) buildBackendIndices(n uint64, backends []*Backend) []uint64 {
	indices := make([]uint64, n)

	switch b.config.Strategy {
	case Random:
		for i := range indices {
			indices[i] = uint64(i)
		}

		for i := len(indices) - 1; i > 0; i-- {
			j := fastrand.Intn(i + 1)
			indices[i], indices[j] = indices[j], indices[i]
		}

	case WeightedRoundRobin:
		for i := range indices {
			indices[i] = uint64(i)
		}

		slices.SortStableFunc(indices, func(i, j uint64) int {
			return backends[j].Weight - backends[i].Weight
		})

	default: // RoundRobin
		start := b.current.Add(1) % n
		for i := range indices {
			indices[i] = (start + uint64(i)) % n
		}
	}

	return indices
}

// Stats returns snapshot health metrics for all registered backends.
func (b *Balancer) Stats() Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	stats := Stats{TotalBackends: len(b.backends)}
	for _, b := range b.backends {
		if b.tracker.IsAvailable() {
			stats.HealthyBackends++
		} else {
			stats.UnhealthyBackends++
		}
	}

	return stats
}

// SetWeight updates the selection weight of a target backend URL.
func (b *Balancer) SetWeight(backendURL string, weight int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, b := range b.backends {
		if b.URL == backendURL {
			b.Weight = weight
			return true
		}
	}

	return false
}

// Reset restores all registered backends to healthy status immediately.
func (b *Balancer) Reset() {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, b := range b.backends {
		b.tracker.Reset()
	}
}

func createBackends(urls []string, cfg Config) []*Backend {
	return generic.Map(urls, func(u string) *Backend {
		parsed, parseErr := url.Parse(u)
		b := &Backend{
			URL:       u,
			Weight:    1,
			client:    &http.Client{Timeout: 15 * time.Second},
			parsedURL: parsed,
			parseErr:  parseErr,
		}

		b.tracker = health.NewTracker(
			u, cfg.MaxFails, cfg.RetryAfter,
			func(name string, fails uint32, retryAfter time.Duration) {
				slog.Warn("backend marked unhealthy", "backend", name, "fails", fails, "retry_after", retryAfter)
			},
			func(name string) {
				slog.Info("backend recovered", "backend", name)
			},
		)

		return b
	})
}

func (b *Balancer) isFault(resp *http.Response, err error) bool {
	if err != nil {
		return !errors.Is(err, context.Canceled)
	}

	if resp != nil {
		sc := resp.StatusCode
		return sc == http.StatusBadGateway || sc == http.StatusGatewayTimeout || sc == http.StatusServiceUnavailable
	}

	return false
}

func (b *Balancer) healthCheckLoop() {
	if b.config.HealthCheckURL == "" {
		return
	}

	b.config.HealthCheckInterval = generic.Coalesce(b.config.HealthCheckInterval, time.Minute)

	ticker := time.NewTicker(b.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
			b.probeUnhealthyBackends()
		}
	}
}

func (b *Balancer) probeUnhealthyBackends() {
	b.mu.RLock()
	backends := b.backends
	b.mu.RUnlock()

	for _, backend := range backends {
		if !backend.tracker.IsAvailable() {
			b.checkHealth(backend)
		}
	}
}

func (b *Balancer) checkHealth(backend *Backend) {
	if backend == nil || backend.client == nil {
		return
	}

	req, err := http.NewRequestWithContext(b.ctx, http.MethodGet, b.config.HealthCheckURL, nil)
	if err != nil {
		return
	}

	resp, err := backend.client.Do(req)
	if err == nil && resp != nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			backend.tracker.MarkSuccess()
		}

		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
}

// Prewarm warms up TCP/TLS connection pools to all registered backends concurrently.
func (b *Balancer) Prewarm(ctx context.Context) {
	b.mu.RLock()
	backends := make([]*Backend, len(b.backends))
	copy(backends, b.backends)
	b.mu.RUnlock()

	var wg sync.WaitGroup
	for _, backend := range backends {
		if backend == nil || backend.client == nil {
			continue
		}

		wg.Add(1)

		go func(bk *Backend) {
			defer wg.Done()

			req, err := http.NewRequestWithContext(ctx, http.MethodHead, bk.URL, nil)
			if err != nil {
				return
			}

			resp, err := bk.client.Do(req)
			if err == nil && resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
		}(backend)
	}

	wg.Wait()
}
