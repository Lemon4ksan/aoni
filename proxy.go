// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/miyako/generic"
)

// ClientWithProxy pairs an [HTTPDoer] with a proxy URL.
type ClientWithProxy struct {
	Client   HTTPDoer
	ProxyURL string
}

// ProxyConfig configures a proxy-supported HTTP client.
type ProxyConfig struct {
	// ProxyURL is the address of the proxy server (e.g. http://user:pass@ip:port).
	ProxyURL string
	// Timeout is the overall request timeout.
	Timeout time.Duration
	// InsecureSkipVerify controls whether SSL/TLS certificate verification is bypassed.
	InsecureSkipVerify bool
	// Transport overrides the default transport settings.
	Transport http.RoundTripper
	// TransportFactory creates a custom [http.RoundTripper].
	TransportFactory func(ProxyConfig) (http.RoundTripper, error)
}

// NewProxyClient creates an [http.Client] configured with proxy transport.
// It prioritizes [ProxyConfig.TransportFactory], then [ProxyConfig.Transport],
// and falls back to a default [http.Transport] if neither is provided.
//
// If [ProxyConfig.ProxyURL] is empty, no proxy routing is applied.
// If [ProxyConfig.Timeout] is zero, a default 15-second timeout is set.
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

			transport.Proxy = http.ProxyURL(u)
		}

		rt = transport
	}

	return &http.Client{
		Transport: rt,
		Timeout:   timeout,
	}, nil
}

// ProxyRotatorConfig configures health-checking and recovery for a [ProxyRotator].
type ProxyRotatorConfig struct {
	// MaxFails is the consecutive error limit before a client is marked unhealthy.
	MaxFails uint32
	// RetryAfter is the duration for which an unhealthy client is kept offline.
	RetryAfter time.Duration
	// HealthCheckURL is the endpoint probed during background health checks.
	HealthCheckURL string
	// HealthCheckInterval sets the frequency of background health checks.
	HealthCheckInterval time.Duration
}

// StickyKeyFunc extracts a session identifier from a request for sticky routing.
// Return an empty string to fall back to round-robin rotation.
type StickyKeyFunc func(req *http.Request) string

// StickyKeyFromCookie returns a function to extract the key from a specific cookie.
func StickyKeyFromCookie(cookieName string) StickyKeyFunc {
	return func(req *http.Request) string {
		if cookie, err := req.Cookie(cookieName); err == nil {
			return cookie.Value
		}

		return ""
	}
}

// StickyKeyFromHeader returns a function to extract the key from the HTTP header.
func StickyKeyFromHeader(headerName string) StickyKeyFunc {
	return func(req *http.Request) string {
		return req.Header.Get(headerName)
	}
}

type trackedClient struct {
	client   HTTPDoer
	proxyURL string
	*HealthTracker
}

type sessionEntry struct {
	clientIdx int
	lastSeen  time.Time
}

// ProxyRotator distributes HTTP requests across a pool of proxy clients.
// It implements [HTTPDoer] and supports sticky routing, health monitoring,
// and dynamic pool replacement.
//
// Create instances with [NewProxyRotator].
type ProxyRotator struct {
	mu            sync.RWMutex
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

// NewProxyRotator creates a [ProxyRotator] from the given config and clients.
// It returns an error if clients is empty.
// Default [ProxyRotatorConfig.MaxFails] is 3; default [ProxyRotatorConfig.RetryAfter] is 30 seconds.
func NewProxyRotator(config ProxyRotatorConfig, clients ...ClientWithProxy) (*ProxyRotator, error) {
	if len(clients) == 0 {
		return nil, errors.New("aoni: proxy rotator requires at least one client")
	}

	config.MaxFails = generic.Coalesce(config.MaxFails, 3)
	config.RetryAfter = generic.Coalesce(config.RetryAfter, 30*time.Second)

	tracked := generic.Map(clients, func(c ClientWithProxy) *trackedClient {
		tc := &trackedClient{client: c.Client, proxyURL: c.ProxyURL}
		tc.HealthTracker = NewHealthTracker(c.ProxyURL, config.MaxFails, config.RetryAfter,
			func(name string, fails uint32, retryAfter time.Duration) {
				slog.Warn("proxy marked unhealthy", //nolint:gosec
					"proxy", name, "fails", fails, "retry_after", retryAfter)
			},
			func(name string) {
				slog.Info("proxy recovered", //nolint:gosec
					"proxy", name)
			},
		)

		return tc
	})

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
		r.config.HealthCheckInterval = generic.Coalesce(config.HealthCheckInterval, time.Minute)

		r.wg.Go(r.healthCheckLoop)
	}

	return r, nil
}

// UpdateClients replaces the active pool and resets all session mappings.
// If clients is empty, it returns without changes.
func (r *ProxyRotator) UpdateClients(clients ...ClientWithProxy) {
	if len(clients) == 0 {
		return
	}

	tracked := generic.Map(clients, func(cp ClientWithProxy) *trackedClient {
		tc := &trackedClient{
			client:   cp.Client,
			proxyURL: cp.ProxyURL,
		}
		tc.HealthTracker = NewHealthTracker(cp.ProxyURL, r.config.MaxFails, r.config.RetryAfter,
			func(name string, fails uint32, retryAfter time.Duration) {
				slog.Warn("proxy marked unhealthy", //nolint:gosec
					"proxy", name, "fails", fails, "retry_after", retryAfter)
			},
			func(name string) {
				slog.Info("proxy recovered", //nolint:gosec
					"proxy", name)
			},
		)

		return tc
	})

	r.mu.Lock()
	r.clients = tracked
	r.current.Store(0)
	r.sessions = make(map[string]*sessionEntry)
	r.mu.Unlock()
}

// Close stops background routines and closes idle connections.
func (r *ProxyRotator) Close() error {
	r.cancel()
	r.wg.Wait()

	for _, tc := range r.clients {
		if httpClient, ok := tc.client.(*http.Client); ok {
			if transport, ok := httpClient.Transport.(*http.Transport); ok {
				transport.CloseIdleConnections()
			}
		}
	}

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

// WithStickySessions returns a copy of r configured with the given key extractor.
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

// Do performs an HTTP request using the next available client in the pool.
// It attempts sticky routing first, then falls back to round-robin selection.
// If a client faults, it is marked unhealthy. Returns an error if all clients fail.
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

	if stickyIdx >= 0 && stickyIdx < len(clients) {
		tc := clients[stickyIdx]
		if r.isAvailable(tc) {
			proxyCtx := context.WithValue(req.Context(), proxyCtxKey{}, tc.proxyURL)

			resp, err := tc.client.Do(req.WithContext(proxyCtx))
			if !r.isProxyFault(resp, err) {
				r.markSuccess(tc)

				return resp, err
			}

			r.markFailed(tc)

			if resp != nil {
				_ = resp.Body.Close()
			}

			lastErr = err
		}
	}

	for range n {
		idx := r.current.Add(1) % n
		if int(idx) == stickyIdx { //nolint:gosec
			continue
		}

		tc := clients[idx]
		if !r.isAvailable(tc) {
			continue
		}

		proxyCtx := context.WithValue(req.Context(), proxyCtxKey{}, tc.proxyURL)
		reqWithProxy := req.WithContext(proxyCtx)

		resp, err := tc.client.Do(reqWithProxy)
		if r.isProxyFault(resp, err) {
			r.markFailed(tc)

			lastErr = err

			if resp != nil {
				_ = resp.Body.Close()
			}

			continue
		}

		r.markSuccess(tc)

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
	return tc.IsAvailable()
}

func (r *ProxyRotator) markFailed(tc *trackedClient) {
	tc.MarkFailed()
}

func (r *ProxyRotator) markSuccess(tc *trackedClient) {
	tc.MarkSuccess()
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

// Prewarm opens TCP/TLS connections to targetURL through all proxy clients.
// It sends concurrent HEAD requests to pre-populate transport connection pools.
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
