// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// DNSResolver is an interface for DNS resolution.
type DNSResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type dnsCacheEntry struct {
	ips    []net.IPAddr
	expiry time.Time
}

// InMemoryDNSCache is an in-memory DNS cache that implements the [DNSResolver] interface.
type InMemoryDNSCache struct {
	mu       sync.RWMutex
	cache    map[string]dnsCacheEntry
	ttl      time.Duration
	resolver DNSResolver
}

// NewInMemoryDNSCache creates a new [InMemoryDNSCache] with the given TTL and resolver.
func NewInMemoryDNSCache(ttl time.Duration, r DNSResolver) *InMemoryDNSCache {
	if r == nil {
		r = &net.Resolver{}
	}

	return &InMemoryDNSCache{
		cache:    make(map[string]dnsCacheEntry),
		ttl:      ttl,
		resolver: r,
	}
}

// LookupIPAddr looks up the IP addresses for the given host using the cache or resolver.
func (c *InMemoryDNSCache) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	c.mu.RLock()
	entry, ok := c.cache[host]
	c.mu.RUnlock()

	if ok && time.Now().Before(entry.expiry) {
		return entry.ips, nil
	}

	ips, err := c.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[host] = dnsCacheEntry{
		ips:    ips,
		expiry: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()

	return ips, nil
}

// DoHResolver is a DNS resolver that uses DNS-over-HTTPS (DoH).
type DoHResolver struct {
	client   *http.Client
	endpoint string // e.g., "https://cloudflare-dns.com/dns-query"
}

// NewDoHResolver creates a new [DoHResolver] with the given endpoint.
func NewDoHResolver(endpoint string) *DoHResolver {
	return &DoHResolver{
		client:   &http.Client{Timeout: 5 * time.Second},
		endpoint: endpoint,
	}
}

type dohResponse struct {
	Answer []struct {
		Type int    `json:"type"`
		Data string `json:"data"`
	} `json:"Answer"`
}

// LookupIPAddr looks up the IP addresses for the given host using the DoH resolver.
func (r *DoHResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	reqURL := fmt.Sprintf("%s?name=%s&type=A", r.endpoint, host)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/dns-json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp dohResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	var ipAddrs []net.IPAddr
	for _, ans := range apiResp.Answer {
		if ans.Type == 1 { // IPv4
			ip := net.ParseIP(ans.Data)
			if ip != nil {
				ipAddrs = append(ipAddrs, net.IPAddr{IP: ip})
			}
		}
	}

	if len(ipAddrs) == 0 {
		return nil, fmt.Errorf("aoni doh: no A records for %s", host)
	}

	return ipAddrs, nil
}
