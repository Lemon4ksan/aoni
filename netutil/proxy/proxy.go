// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package proxy provides proxy rotation, SOCKS5/HTTP tunnel dialing, and proxy-isolated TLS session ticket caching.
package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/miyako/generic"
	"github.com/lemon4ksan/miyako/log"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/internal/health"
)

// WithProxyAwareSessionCache enables the proxy-aware TLS session ticket cache.
func WithProxyAwareSessionCache() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.SessionCache = NewProxyAwareSessionCache()
	}
}

// Parse parses a proxy string and detects the protocol.
func Parse(proxyStr string) (*url.URL, error) {
	if proxyStr == "" {
		return nil, errors.New("empty proxy string")
	}

	if strings.Contains(proxyStr, "://") {
		return url.Parse(proxyStr)
	}

	u, err := url.Parse("http://" + proxyStr)
	if err != nil {
		return nil, err
	}

	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		host = u.Host
		portStr = ""
	}

	scheme := "http"

	addr := net.JoinHostPort(host, portStr)
	if portStr == "" {
		addr = host
	}

	if portStr != "" {
		dialer := net.Dialer{Timeout: 300 * time.Millisecond}

		conn, err := dialer.DialContext(context.Background(), "tcp", addr)
		if err == nil {
			defer conn.Close()

			_ = conn.SetDeadline(time.Now().Add(200 * time.Millisecond))

			_, err = conn.Write([]byte{0x05, 0x01, 0x00})
			if err == nil {
				resp := make([]byte, 2)

				n, err := conn.Read(resp)
				if err == nil && n == 2 && resp[0] == 0x05 {
					scheme = "socks5h"
				}
			}
		} else if portStr == "1080" || portStr == "1081" || portStr == "9050" || portStr == "9051" || portStr == "10808" {
			scheme = "socks5h"
		}
	}

	u.Scheme = scheme

	return u, nil
}

// WithClient pairs an [HTTPDoer] with a proxy URL.
type WithClient struct {
	Client   aoni.HTTPDoer
	ProxyURL string
}

// Config configures a proxy-supported HTTP client.
type Config struct {
	// ProxyURL is the address of the proxy server (e.g. http://user:pass@ip:port).
	ProxyURL string
	// Timeout is the overall request timeout.
	Timeout time.Duration
	// InsecureSkipVerify controls whether SSL/TLS certificate verification is bypassed.
	InsecureSkipVerify bool
	// Transport overrides the default transport settings.
	Transport http.RoundTripper
	// TransportFactory creates a custom [http.RoundTripper].
	TransportFactory func(Config) (http.RoundTripper, error)
}

// NewProxyClient creates an [http.Client] configured with proxy transport.
// It prioritizes [Config.TransportFactory], then [Config.Transport],
// and falls back to a default [http.Transport] if neither is provided.
//
// If [Config.ProxyURL] is empty, no proxy routing is applied.
// If [Config.Timeout] is zero, a default 15-second timeout is set.
func NewProxyClient(cfg Config) (*http.Client, error) {
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

// RotatorConfig configures health-checking and recovery for a [Rotator].
type RotatorConfig struct {
	// MaxFails is the consecutive error limit before a client is marked unhealthy.
	MaxFails uint32
	// RetryAfter is the duration for which an unhealthy client is kept offline.
	RetryAfter time.Duration
	// HealthCheckURL is the endpoint probed during background health checks.
	HealthCheckURL string
	// HealthCheckInterval sets the frequency of background health checks.
	HealthCheckInterval time.Duration
	// Logger is the logger used by the proxy rotator.
	Logger aoni.Logger
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
	client          aoni.HTTPDoer
	proxyURL        string
	tracker         *health.Tracker
	domainMu        sync.RWMutex
	domainCooldowns map[string]time.Time
}

func (tc *trackedClient) IsDomainCooledDown(domain string) bool {
	tc.domainMu.RLock()
	defer tc.domainMu.RUnlock()

	if tc.domainCooldowns == nil {
		return false
	}

	cooldownUntil, exists := tc.domainCooldowns[domain]
	if !exists {
		return false
	}

	return time.Now().Before(cooldownUntil)
}

func (tc *trackedClient) PutDomainInCooldown(domain string, duration time.Duration) {
	tc.domainMu.Lock()
	defer tc.domainMu.Unlock()

	if tc.domainCooldowns == nil {
		tc.domainCooldowns = make(map[string]time.Time)
	}

	now := time.Now()
	for d, until := range tc.domainCooldowns {
		if now.After(until) {
			delete(tc.domainCooldowns, d)
		}
	}

	tc.domainCooldowns[domain] = now.Add(duration)
}

type sessionEntry struct {
	clientIdx int
	lastSeen  time.Time
}

// Rotator distributes HTTP requests across a pool of proxy clients.
// It implements [HTTPDoer] and supports sticky routing, health monitoring,
// and dynamic pool replacement.
//
// Create instances with [NewRotator].
type Rotator struct {
	mu            sync.RWMutex
	clients       []*trackedClient
	cfg           RotatorConfig
	current       atomic.Uint32
	stickyKeyFunc StickyKeyFunc
	sessions      map[string]*sessionEntry
	sessionTTL    time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewRotator creates a [Rotator] from the given config and clients.
// It returns an error if clients is empty.
// Default [RotatorConfig.MaxFails] is 3; default [RotatorConfig.RetryAfter] is 30 seconds.
func NewRotator(cfg RotatorConfig, clients ...WithClient) (*Rotator, error) {
	if len(clients) == 0 {
		return nil, errors.New("aoni: proxy rotator requires at least one client")
	}

	cfg.MaxFails = generic.Coalesce(cfg.MaxFails, 3)
	cfg.RetryAfter = generic.Coalesce(cfg.RetryAfter, 30*time.Second)

	if cfg.Logger == nil {
		cfg.Logger = log.Discard
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := &Rotator{
		ctx:        ctx,
		cancel:     cancel,
		cfg:        cfg,
		sessions:   make(map[string]*sessionEntry),
		sessionTTL: 24 * time.Hour,
	}

	r.clients = generic.Map(clients, r.setupClient)

	r.wg.Go(r.cleanupSessionsLoop)

	if cfg.HealthCheckURL != "" {
		r.cfg.HealthCheckInterval = generic.Coalesce(cfg.HealthCheckInterval, time.Minute)

		r.wg.Go(r.healthCheckLoop)
	}

	return r, nil
}

// NewProxyRotatorFromStrings creates a [Rotator] from a list of raw proxy URL strings.
// It automatically instantiates standard http.Clients configured with the respective proxies.
// This is a high-level helper to easily set up a rotating pool from proxy list configurations.
func NewProxyRotatorFromStrings(config RotatorConfig, proxyURLs ...string) (*Rotator, error) {
	if len(proxyURLs) == 0 {
		return nil, errors.New("aoni: proxy rotator requires at least one client")
	}

	var clients []WithClient
	for _, pStr := range proxyURLs {
		u, err := url.Parse(pStr)
		if err != nil {
			return nil, fmt.Errorf("aoni: invalid proxy URL %q: %w", pStr, err)
		}

		httpClient, err := NewProxyClient(Config{
			ProxyURL: pStr,
		})
		if err != nil {
			return nil, err
		}

		clients = append(clients, WithClient{
			Client:   httpClient,
			ProxyURL: u.String(),
		})
	}

	return NewRotator(config, clients...)
}

// RotatorStats provides real-time state metrics for the rotating proxy pool.
type RotatorStats struct {
	TotalProxies     int
	HealthyProxies   int
	UnhealthyProxies int
}

// WithStickySessions returns a copy of r configured with the given key extractor.
func (r *Rotator) WithStickySessions(f StickyKeyFunc) *Rotator {
	c := &Rotator{
		ctx:           r.ctx,
		cancel:        r.cancel,
		clients:       make([]*trackedClient, len(r.clients)),
		cfg:           r.cfg,
		sessions:      make(map[string]*sessionEntry),
		sessionTTL:    r.sessionTTL,
		stickyKeyFunc: f,
	}
	copy(c.clients, r.clients)
	c.current.Store(r.current.Load())

	return c
}

// Prewarm opens TCP/TLS connections to targetURL through all proxy clients.
// It sends concurrent HEAD requests to pre-populate transport connection pools.
func (r *Rotator) Prewarm(ctx context.Context, targetURL string) {
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

// Stats evaluates and returns current health statistics of the registered proxy clients.
func (r *Rotator) Stats() RotatorStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := RotatorStats{TotalProxies: len(r.clients)}
	for _, c := range r.clients {
		if c.tracker.IsAvailable() {
			stats.HealthyProxies++
		} else {
			stats.UnhealthyProxies++
		}
	}

	return stats
}

// Reset clears failure states and restores all registered proxy clients to a healthy state.
func (r *Rotator) Reset() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, c := range r.clients {
		c.tracker.Reset()
	}
}

// ResetDomainCooldowns clears all domain-specific cooldowns for all proxies in the pool.
func (r *Rotator) ResetDomainCooldowns() {
	r.mu.RLock()
	clients := r.clients
	r.mu.RUnlock()

	for _, tc := range clients {
		tc.domainMu.Lock()
		tc.domainCooldowns = make(map[string]time.Time)
		tc.domainMu.Unlock()
	}
}

// UpdateClients replaces the active pool and resets all session mappings.
func (r *Rotator) UpdateClients(clients ...WithClient) {
	if len(clients) == 0 {
		return
	}

	tracked := generic.Map(clients, r.setupClient)

	r.mu.Lock()
	r.clients = tracked
	r.current.Store(0)
	r.sessions = make(map[string]*sessionEntry)
	r.mu.Unlock()
}

// Do performs an HTTP request using the next available client in the pool.
// It attempts sticky routing first, then falls back to round-robin selection.
// If a client faults, it is marked unhealthy. Returns an error if all clients fail.
func (r *Rotator) Do(req *http.Request) (*http.Response, error) {
	r.mu.RLock()
	clients := r.clients
	r.mu.RUnlock()

	var (
		lastErr       error
		n             = uint32(len(clients)) //nolint:gosec
		sessionID     string
		stickyIdx     = -1
		hasCooledDown = false
	)

	domain := req.URL.Hostname()

	if r.stickyKeyFunc != nil {
		if sessionID = r.stickyKeyFunc(req); sessionID != "" {
			stickyIdx = r.getStickyClientIndex(sessionID)
		}
	}

	if stickyIdx >= 0 && stickyIdx < len(clients) {
		tc := clients[stickyIdx]
		if tc.tracker.IsAvailable() && !tc.IsDomainCooledDown(domain) {
			resp, err := r.executeWithProxy(req, tc)
			if r.handleProxyResult(tc, resp, err, domain) {
				return resp, err
			}

			lastErr = err
		}
	}

	for range n {
		idx := r.current.Add(1) % n
		if int(idx) == stickyIdx {
			continue
		}

		tc := clients[idx]
		if !tc.tracker.IsAvailable() {
			continue
		}

		if tc.IsDomainCooledDown(domain) {
			hasCooledDown = true
			continue
		}

		resp, err := r.executeWithProxy(req, tc)
		if !r.handleProxyResult(tc, resp, err, domain) {
			lastErr = err
			continue
		}

		if sessionID != "" {
			r.saveSession(sessionID, int(idx))
		}

		return resp, err
	}

	if lastErr != nil {
		return nil, fmt.Errorf("aoni: all proxies failed, last error: %w", lastErr)
	}

	if hasCooledDown {
		return nil, fmt.Errorf("aoni: all proxies are in cooldown for domain %s", domain)
	}

	return nil, errors.New("aoni: no healthy proxies available")
}

// Close stops background routines and closes idle connections.
func (r *Rotator) Close() error {
	r.cancel()
	r.wg.Wait()

	r.mu.RLock()
	clients := r.clients
	r.mu.RUnlock()

	for _, tc := range clients {
		if httpClient, ok := tc.client.(*http.Client); ok {
			if transport, ok := httpClient.Transport.(*http.Transport); ok {
				transport.CloseIdleConnections()
			}
		}
	}

	return nil
}

func (r *Rotator) setupClient(cp WithClient) *trackedClient {
	tc := &trackedClient{
		client:          cp.Client,
		proxyURL:        cp.ProxyURL,
		domainCooldowns: make(map[string]time.Time),
	}
	tc.tracker = health.NewTracker(cp.ProxyURL, r.cfg.MaxFails, r.cfg.RetryAfter,
		func(name string, fails uint32, retryAfter time.Duration) {
			r.cfg.Logger.Warn("proxy marked unhealthy", "proxy", name, "fails", fails, "retry_after", retryAfter)
		},
		func(name string) {
			r.cfg.Logger.Info("proxy recovered", "proxy", name)
		},
	)

	return tc
}

func (r *Rotator) isProxyFault(resp *http.Response, err error) bool {
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

func (r *Rotator) getStickyClientIndex(sessionID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	if val, ok := r.sessions[sessionID]; ok {
		val.lastSeen = time.Now()
		return val.clientIdx
	}

	return -1
}

func (r *Rotator) executeWithProxy(req *http.Request, tc *trackedClient) (*http.Response, error) {
	proxyCtx := cookie.WithProxyAddress(req.Context(), tc.proxyURL)
	reqWithProxy := req.WithContext(proxyCtx)
	return tc.client.Do(reqWithProxy)
}

func (r *Rotator) handleProxyResult(tc *trackedClient, resp *http.Response, err error, domain string) bool {
	if !r.isProxyFault(resp, err) {
		tc.tracker.MarkSuccess()

		if err == nil && resp != nil && resp.StatusCode == http.StatusForbidden {
			tc.PutDomainInCooldown(domain, 10*time.Minute)
		}

		return true
	}

	tc.tracker.MarkFailed()

	if resp != nil {
		_ = resp.Body.Close()
	}

	return false
}

func (r *Rotator) saveSession(sessionID string, idx int) {
	r.mu.Lock()
	r.sessions[sessionID] = &sessionEntry{
		clientIdx: idx,
		lastSeen:  time.Now(),
	}
	r.mu.Unlock()
}

func (r *Rotator) healthCheckLoop() {
	if r.cfg.HealthCheckURL == "" {
		return
	}

	ticker := time.NewTicker(r.cfg.HealthCheckInterval)
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
				if tc.tracker.IsAvailable() {
					r.checkHealth(tc)
				}
			}
		}
	}
}

func (r *Rotator) checkHealth(tc *trackedClient) {
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.cfg.HealthCheckURL, nil)
	if err != nil {
		return
	}

	resp, err := tc.client.Do(req)
	if err == nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			tc.tracker.MarkSuccess()
		}

		_ = resp.Body.Close()
	}
}

func (r *Rotator) cleanupSessionsLoop() {
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
