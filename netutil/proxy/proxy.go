// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package proxy provides proxy pool rotation, SOCKS5/HTTP tunnel dialing, and proxy-isolated TLS session ticket management.
package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/foundation/async/log"
	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/silicon/clock"
	"golang.org/x/sys/cpu"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/internal/health"
	"github.com/lemon4ksan/aoni/netutil/trie"
)

var (
	// ErrNoProxyClients is returned when instantiating a [Rotator] with zero proxy clients.
	ErrNoProxyClients = errors.New("aoni: proxy rotator requires at least one client")

	// ErrNoHealthyProxies is returned when all registered proxy exit nodes are unhealthy or in cooldown.
	ErrNoHealthyProxies = errors.New("aoni: no healthy proxies available")
)

// WithAwareSessionCache enables the proxy-isolated TLS session ticket cache.
func WithAwareSessionCache() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.SessionCache = NewProxyAwareSessionCache()
	}
}

// Parse parses a raw proxy string and detects the target transport protocol.
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

// WithClient pairs an [aoni.HTTPDoer] client with its associated exit proxy URL.
type WithClient struct {
	Client   aoni.HTTPDoer
	ProxyURL string
}

// Config configures proxy client parameters.
type Config struct {
	ProxyURL           string
	Timeout            time.Duration
	InsecureSkipVerify bool
	Transport          http.RoundTripper
	TransportFactory   func(Config) (http.RoundTripper, error)
}

// NewClient instantiates an [*http.Client] configured with proxy transport routing.
func NewClient(cfg Config) (*http.Client, error) {
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
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify}, //nolint:gosec
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

// RotatorConfig configures health checking, failure thresholds, and recovery durations for a [Rotator].
type RotatorConfig struct {
	MaxFails            uint32
	RetryAfter          time.Duration
	HealthCheckURL      string
	HealthCheckInterval time.Duration
	Logger              core.Logger
}

// StickyKeyFunc extracts a sticky session identifier string from an incoming request.
type StickyKeyFunc func(req *http.Request) string

// StickyKeyFromCookie constructs a [StickyKeyFunc] extracting session keys from cookieName.
func StickyKeyFromCookie(cookieName string) StickyKeyFunc {
	return func(req *http.Request) string {
		if cookie, err := req.Cookie(cookieName); err == nil {
			return cookie.Value
		}

		return ""
	}
}

// StickyKeyFromHeader constructs a [StickyKeyFunc] extracting session keys from headerName.
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

	return exists && time.Now().Before(cooldownUntil)
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

// Rotator balances requests across a pool of proxy clients, supporting sticky sessions, health probing, and domain cooldowns.
type Rotator struct {
	stickyKeyFunc StickyKeyFunc
	sessions      map[string]*sessionEntry
	clients       []*trackedClient
	ctx           context.Context
	cancel        context.CancelFunc
	sessionTTL    time.Duration
	cfg           RotatorConfig
	wg            sync.WaitGroup

	_       cpu.CacheLinePad
	mu      sync.RWMutex
	_       cpu.CacheLinePad
	current atomic.Uint32
	_       cpu.CacheLinePad
}

// NewRotator instantiates a proxy pool [Rotator] using configured clients.
func NewRotator(cfg RotatorConfig, clients ...WithClient) (*Rotator, error) {
	if len(clients) == 0 {
		return nil, ErrNoProxyClients
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

// NewRotatorFromStrings constructs a [Rotator] from raw proxy URL strings.
func NewRotatorFromStrings(config RotatorConfig, proxyURLs ...string) (*Rotator, error) {
	if len(proxyURLs) == 0 {
		return nil, ErrNoProxyClients
	}

	clients := make([]WithClient, 0, len(proxyURLs))
	for _, pStr := range proxyURLs {
		u, err := url.Parse(pStr)
		if err != nil {
			return nil, fmt.Errorf("aoni: invalid proxy URL %q: %w", pStr, err)
		}

		httpClient, err := NewClient(Config{ProxyURL: pStr})
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

// RotatorStats holds snapshot health metrics for the proxy pool.
type RotatorStats struct {
	TotalProxies     int
	HealthyProxies   int
	UnhealthyProxies int
}

// WithStickySessions returns a copy of r configured with sticky session key extraction.
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

// Prewarm opens TCP/TLS connections to targetURL across all proxy clients in parallel to pre-populate transport connection pools.
func (r *Rotator) Prewarm(ctx context.Context, targetURL string) {
	r.mu.RLock()
	clients := slices.Clone(r.clients)
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
			if err == nil && resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
		}(tc)
	}

	wg.Wait()
}

// Stats evaluates and returns current health and availability metrics for all registered proxy clients.
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

// Reset clears failure counters and restores all registered proxy clients to healthy status immediately.
func (r *Rotator) Reset() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, c := range r.clients {
		c.tracker.Reset()
	}
}

// ResetDomainCooldowns purges all active domain-specific cooldown blocks across every proxy client in the pool.
func (r *Rotator) ResetDomainCooldowns() {
	r.mu.RLock()
	clients := r.clients
	r.mu.RUnlock()

	for _, tc := range clients {
		tc.domainMu.Lock()
		clear(tc.domainCooldowns)
		tc.domainMu.Unlock()
	}
}

// UpdateClients dynamically replaces the active proxy client pool and resets all sticky session mappings.
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

// Do executes req using the next available proxy client in the pool.
func (r *Rotator) Do(req *http.Request) (*http.Response, error) {
	r.mu.RLock()
	clients := r.clients
	r.mu.RUnlock()

	n := uint32(len(clients)) //nolint:gosec
	if n == 0 {
		return nil, ErrNoProxyClients
	}

	var (
		lastErr       error
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

	mask := n - 1
	isPowerOfTwo := (n & mask) == 0

	for range n {
		var idx uint32
		if isPowerOfTwo {
			idx = (r.current.Add(1) - 1) & mask
		} else {
			idx = r.current.Add(1) % n
		}

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

	return nil, ErrNoHealthyProxies
}

// Close stops background health check workers and closes idle transport connections.
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
		return !errors.Is(err, context.Canceled)
	}

	if resp != nil {
		sc := resp.StatusCode

		return sc == http.StatusProxyAuthRequired ||
			sc == http.StatusTooManyRequests ||
			sc == http.StatusBadGateway ||
			sc == http.StatusGatewayTimeout ||
			sc == http.StatusServiceUnavailable
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

			now := clock.CoarseTime()
			for k, v := range r.sessions {
				if now.Sub(v.lastSeen) > r.sessionTTL {
					delete(r.sessions, k)
				}
			}

			r.mu.Unlock()
		}
	}
}

// DomainProxyRouter maps domain patterns (e.g., "*.google.com", "api.target.com") to target proxy URLs
// using an O(K) reverse domain radix tree trie instead of regular expressions.
type DomainProxyRouter struct {
	rt *trie.ReverseDomainTrie[*url.URL]
}

// NewDomainProxyRouter instantiates a new [DomainProxyRouter].
func NewDomainProxyRouter() *DomainProxyRouter {
	return &DomainProxyRouter{
		rt: trie.NewReverseDomainTrie[*url.URL](),
	}
}

// AddRoute registers a domain pattern (e.g. "*.example.com" or "api.target.org") mapped to proxyURL.
func (r *DomainProxyRouter) AddRoute(pattern string, proxyURL *url.URL) {
	if r != nil && r.rt != nil {
		r.rt.Insert(pattern, proxyURL)
	}
}

// RouteForDomain matches targetDomain against registered patterns in O(K) time and yields the matching proxy URL.
func (r *DomainProxyRouter) RouteForDomain(targetDomain string) (*url.URL, bool) {
	if r == nil || r.rt == nil {
		return nil, false
	}

	return r.rt.Match(targetDomain)
}

// RouteForDomainOptional matches targetDomain and returns a Swift-inspired [generic.Optional].
func (r *DomainProxyRouter) RouteForDomainOptional(targetDomain string) generic.Optional[*url.URL] {
	u, ok := r.RouteForDomain(targetDomain)
	if !ok {
		return generic.None[*url.URL]()
	}

	return generic.Some(u)
}

// FindClient searches for a proxy client matching the predicate
// and returns a Swift-inspired [generic.Optional].
func (r *Rotator) FindClient(predicate func(aoni.HTTPDoer) bool) generic.Optional[aoni.HTTPDoer] {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, tc := range r.clients {
		if predicate(tc.client) {
			return generic.Some(tc.client)
		}
	}

	return generic.None[aoni.HTTPDoer]()
}
