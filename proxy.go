// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// ProxyConfig contains parameters for configuring an HTTP client with proxy support.
type ProxyConfig struct {
	ProxyURL           string            // Format: http://user:pass@ip:port or socks5://ip:port
	Timeout            time.Duration     // Overall request timeout (recommended 15-30s for proxies)
	InsecureSkipVerify bool              // Disable SSL verification. Overidden by Transport or TransportFactory if provided
	Transport          http.RoundTripper // Custom transport/RoundTripper if provided

	TransportFactory func(ProxyConfig) (http.RoundTripper, error) // Custom transport factory
}

// NewProxyClient creates a standard *http.Client configured to work through a proxy.
// It safely manages the connection pool to avoid memory leaks when running bots.
func NewProxyClient(cfg ProxyConfig) (*http.Client, error) {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}

	var rt http.RoundTripper

	switch {
	case cfg.TransportFactory != nil:
		var err error

		rt, err = cfg.TransportFactory(cfg)
		if err != nil {
			return nil, fmt.Errorf("aoni: custom transport factory: %w", err)
		}

	case cfg.Transport != nil:
		rt = cfg.Transport
	default:
		transport := &http.Transport{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			// #nosec G402 -- InsecureSkipVerify is configurable by the user for proxy compatibility.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
		}

		if cfg.ProxyURL != "" {
			u, err := url.Parse(cfg.ProxyURL)
			if err != nil {
				return nil, fmt.Errorf("aoni: invalid proxy URL %q: %w", cfg.ProxyURL, err)
			}

			// Go natively supports http://, https:// and socks5://
			transport.Proxy = http.ProxyURL(u)
		}

		rt = transport
	}

	return &http.Client{
		Transport: rt,
		Timeout:   timeout,
	}, nil
}

// ProxyRotatorConfig defines proxy health checking parameters.
type ProxyRotatorConfig struct {
	// MaxFails is the number of sequential errors allowed before the proxy is marked unhealthy.
	MaxFails uint32
	// RetryAfter is the duration for which an unhealthy proxy is excluded from rotation.
	RetryAfter time.Duration
	// HealthCheckURL is the endpoint used for background proxy health checks.
	HealthCheckURL string
	// HealthCheckInterval is the interval at which background health checks run.
	HealthCheckInterval time.Duration
}

// StickyKeyFunc defines how to extract a session identifier from a request.
// If it returns an empty string, the request is handled via standard Round-Robin.
type StickyKeyFunc func(req *http.Request) string

type trackedClient struct {
	client      HTTPDoer
	failCount   atomic.Uint32
	unhealthy   atomic.Bool
	recoveredAt atomic.Int64
}

type sessionEntry struct {
	clientIdx int
	lastSeen  time.Time
}

// ProxyRotator allows distributing requests between multiple proxies.
// It implements the [HTTPDoer] interface and can be passed to [NewClient].
//
// Create new instances of ProxyRotator using the [NewProxyRotator] constructor.
// It supports round-robin scheduling, health monitoring, and sticky sessions via [StickyKeyFunc].
type ProxyRotator struct {
	mu            sync.RWMutex // Protects clients slice and sessions map
	clients       []*trackedClient
	config        ProxyRotatorConfig
	current       atomic.Uint64
	stickyKeyFunc StickyKeyFunc
	sessions      map[string]*sessionEntry
	sessionTTL    time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewProxyRotator initializes the rotator (Round-Robin).
func NewProxyRotator(config ProxyRotatorConfig, clients ...HTTPDoer) (*ProxyRotator, error) {
	if len(clients) == 0 {
		return nil, errors.New("aoni: proxy rotator requires at least one client")
	}

	if config.MaxFails == 0 {
		config.MaxFails = 3
	}

	if config.RetryAfter == 0 {
		config.RetryAfter = 30 * time.Second
	}

	tracked := make([]*trackedClient, len(clients))
	for i, c := range clients {
		tracked[i] = &trackedClient{client: c}
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := &ProxyRotator{
		ctx:        ctx,
		cancel:     cancel,
		clients:    tracked,
		config:     config,
		sessions:   make(map[string]*sessionEntry),
		sessionTTL: 24 * time.Hour,
	}

	r.wg.Go(r.cleanupSessionsLoop)

	if config.HealthCheckURL != "" {
		if config.HealthCheckInterval == 0 {
			r.config.HealthCheckInterval = 1 * time.Minute
		}

		r.wg.Go(r.healthCheckLoop)
	}

	return r, nil
}

// UpdateClients replaces the current set of proxy clients with a new one.
// This operation is thread-safe and resets existing sticky session mappings
// to prevent indexing errors.
func (r *ProxyRotator) UpdateClients(clients ...HTTPDoer) {
	if len(clients) == 0 {
		return
	}

	tracked := make([]*trackedClient, len(clients))
	for i, c := range clients {
		tracked[i] = &trackedClient{client: c}
	}

	r.mu.Lock()
	r.clients = tracked
	r.current.Store(0)
	r.sessions = make(map[string]*sessionEntry)
	r.mu.Unlock()
}

// Close stops background health checks.
func (r *ProxyRotator) Close() error {
	r.cancel()
	r.wg.Wait()

	return nil
}

func (r *ProxyRotator) healthCheckLoop() {
	ticker := time.NewTicker(r.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.mu.RLock()
			clients := r.clients
			r.mu.RUnlock()

			for _, tc := range clients {
				if tc.unhealthy.Load() {
					r.checkHealth(tc)
				}
			}
		}
	}
}

func (r *ProxyRotator) checkHealth(tc *trackedClient) {
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.config.HealthCheckURL, nil)
	if err != nil {
		return
	}

	resp, err := tc.client.Do(req)
	if err == nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			r.markSuccess(tc)
		}

		_ = resp.Body.Close()
	}
}

func (r *ProxyRotator) cleanupSessionsLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.mu.Lock()

			now := time.Now()
			for k, v := range r.sessions {
				if now.Sub(v.lastSeen) > r.sessionTTL {
					delete(r.sessions, k)
				}
			}

			r.mu.Unlock()
		}
	}
}

// WithStickySessions enables sticky sessions using the provided key extractor.
// Returns the copy of the proxy rotator with the sticky key function set.
func (r *ProxyRotator) WithStickySessions(f StickyKeyFunc) *ProxyRotator {
	c := &ProxyRotator{
		ctx:           r.ctx,
		cancel:        r.cancel,
		clients:       make([]*trackedClient, len(r.clients)),
		config:        r.config,
		sessions:      make(map[string]*sessionEntry),
		sessionTTL:    r.sessionTTL,
		stickyKeyFunc: f,
	}
	copy(c.clients, r.clients)
	c.current.Store(r.current.Load())

	return c
}

// Do performs an HTTP request using the next available client in the rotation.
//
// If all proxies in the pool fail or are marked unhealthy, Do returns an error
// indicating that no healthy proxies are available.
func (r *ProxyRotator) Do(req *http.Request) (*http.Response, error) {
	r.mu.RLock()
	clients := r.clients
	r.mu.RUnlock()

	var (
		lastErr   error
		n         = uint64(len(clients))
		sessionID string
		stickyIdx = -1
	)

	// Attempt to extract session ID and find a "stuck" proxy
	if r.stickyKeyFunc != nil {
		sessionID = r.stickyKeyFunc(req)
		if sessionID != "" {
			r.mu.Lock()
			if val, ok := r.sessions[sessionID]; ok {
				stickyIdx = val.clientIdx
				val.lastSeen = time.Now()
			}

			r.mu.Unlock()
		}
	}

	// Try the sticky proxy first if it's available
	if stickyIdx >= 0 && stickyIdx < len(clients) {
		tc := clients[stickyIdx]
		if r.isAvailable(tc) {
			resp, err := tc.client.Do(req)
			if !r.isProxyFault(resp, err) {
				r.markSuccess(tc)

				return resp, err
			}

			// Sticky proxy failed, mark it and move to general rotation
			r.markFailed(tc)

			if resp != nil {
				_ = resp.Body.Close()
			}

			lastErr = err
		}
	}

	// General Round-Robin rotation
	for range n {
		idx := r.current.Add(1) % n
		if int(idx) == stickyIdx { //nolint:gosec
			continue // Already tried above
		}

		tc := clients[idx]
		if !r.isAvailable(tc) {
			continue
		}

		resp, err := tc.client.Do(req)
		if r.isProxyFault(resp, err) {
			r.markFailed(tc)

			lastErr = err

			if resp != nil {
				_ = resp.Body.Close()
			}

			continue
		}

		r.markSuccess(tc)

		// Update or set the sticky association for future requests
		if sessionID != "" {
			r.mu.Lock()
			r.sessions[sessionID] = &sessionEntry{
				clientIdx: int(idx), //nolint:gosec
				lastSeen:  time.Now(),
			}
			r.mu.Unlock()
		}

		return resp, err
	}

	if lastErr != nil {
		return nil, fmt.Errorf("aoni: all proxies failed, last error: %w", lastErr)
	}

	return nil, errors.New("aoni: no healthy proxies available")
}

func (r *ProxyRotator) isAvailable(tc *trackedClient) bool {
	if !tc.unhealthy.Load() {
		return true
	}

	if time.Now().UnixNano() >= tc.recoveredAt.Load() {
		return true
	}

	return false
}

func (r *ProxyRotator) markFailed(tc *trackedClient) {
	fails := tc.failCount.Add(1)
	if fails >= r.config.MaxFails {
		tc.unhealthy.Store(true)

		recoveryTime := time.Now().Add(r.config.RetryAfter).UnixNano()
		tc.recoveredAt.Store(recoveryTime)
	}
}

func (r *ProxyRotator) markSuccess(tc *trackedClient) {
	tc.failCount.Store(0)
	tc.unhealthy.Store(false)
}

func (r *ProxyRotator) isProxyFault(resp *http.Response, err error) bool {
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
		if resp.StatusCode == http.StatusProxyAuthRequired { // 407
			return true
		}

		if resp.StatusCode == http.StatusTooManyRequests { // 429
			return true
		}

		if resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusGatewayTimeout ||
			resp.StatusCode == http.StatusServiceUnavailable {
			return true
		}
	}

	return false
}

// Prewarm establishes TCP/TLS connections to targetURL through all proxies in the rotator.
// It sends a fast parallel HEAD request to targetURL through each proxy client to pre-populate the connection pool.
func (r *ProxyRotator) Prewarm(ctx context.Context, targetURL string) {
	r.mu.RLock()
	clients := make([]*trackedClient, len(r.clients))
	copy(clients, r.clients)
	r.mu.RUnlock()

	var wg sync.WaitGroup
	for _, tc := range clients {
		wg.Add(1)

		go func(c *trackedClient) {
			defer wg.Done()

			req, err := http.NewRequestWithContext(ctx, http.MethodHead, targetURL, nil)
			if err != nil {
				return
			}

			resp, err := c.client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
			}
		}(tc)
	}

	wg.Wait()
}
