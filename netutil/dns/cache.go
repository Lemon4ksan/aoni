// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/async/dedup"
	"github.com/lemon4ksan/foundation/net/dns/wire"
	"github.com/lemon4ksan/foundation/silicon/clock"
)

var evictInterval = time.Minute

type dnsCacheEntry struct {
	ips    []net.IPAddr
	expiry time.Time
}

// InMemoryDNSCache manages thread-safe, in-memory caching of DNS resolutions,
// adhering to authoritative TTLs or fallback durations.
type InMemoryDNSCache struct {
	mu       sync.RWMutex
	cache    map[string]dnsCacheEntry
	ttl      time.Duration
	resolver Resolver
	sflight  dedup.Group[string, []net.IPAddr]
	cancel   context.CancelFunc
}

// NewInMemoryDNSCache creates an InMemoryDNSCache and launches background cache eviction.
func NewInMemoryDNSCache(ttl time.Duration, r Resolver) *InMemoryDNSCache {
	if r == nil {
		r = &net.Resolver{}
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &InMemoryDNSCache{
		cache:    make(map[string]dnsCacheEntry),
		ttl:      ttl,
		resolver: r,
		cancel:   cancel,
	}

	go c.evictionLoop(ctx)

	return c
}

// Close terminates the background cache eviction goroutine.
func (c *InMemoryDNSCache) Close() {
	c.cancel()
}

func (c *InMemoryDNSCache) evictionLoop(ctx context.Context) {
	ticker := time.NewTicker(evictInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.purgeExpired()
		}
	}
}

func (c *InMemoryDNSCache) purgeExpired() {
	c.mu.Lock()

	now := clock.CoarseTime()
	for k, v := range c.cache {
		if now.After(v.expiry) {
			delete(c.cache, k)
		}
	}

	c.mu.Unlock()
}

// LookupIPAddr resolves host using cached TTL entries or queries the underlying resolver.
func (c *InMemoryDNSCache) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	c.mu.RLock()
	entry, ok := c.cache[host]
	c.mu.RUnlock()

	if ok && clock.CoarseTime().Before(entry.expiry) {
		return entry.ips, nil
	}

	ips, err := c.sflight.Do(ctx, host, func(ctx context.Context) ([]net.IPAddr, error) {
		c.mu.RLock()
		entry, ok := c.cache[host]
		c.mu.RUnlock()

		if ok && clock.CoarseTime().Before(entry.expiry) {
			return entry.ips, nil
		}

		if extendedResolver, ok := c.resolver.(interface {
			LookupDNSRecords(ctx context.Context, host string) ([]wire.DNSRecord, error)
		}); ok {
			records, err := extendedResolver.LookupDNSRecords(ctx, host)
			if err == nil && len(records) > 0 {
				return c.storeRecords(host, records)
			}
		}

		resolvedIPs, err := c.resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}

		c.mu.Lock()
		c.cache[host] = dnsCacheEntry{
			ips:    resolvedIPs,
			expiry: time.Now().Add(c.ttl),
		}
		c.mu.Unlock()

		return resolvedIPs, nil
	})
	if err != nil {
		return nil, wrapDNSError(host, "InMemoryCache", "", err)
	}

	return ips, nil
}

func (c *InMemoryDNSCache) storeRecords(host string, records []wire.DNSRecord) ([]net.IPAddr, error) {
	var minTTL uint32 = 3600

	ips := make([]net.IPAddr, len(records))

	for i, r := range records {
		ips[i] = net.IPAddr{IP: r.Addr.AsSlice()}
		if r.TTL > 0 && r.TTL < minTTL {
			minTTL = r.TTL
		}
	}

	effectiveTTL := time.Duration(max(minTTL, 5)) * time.Second

	c.mu.Lock()
	c.cache[host] = dnsCacheEntry{
		ips:    ips,
		expiry: time.Now().Add(effectiveTTL),
	}
	c.mu.Unlock()

	return ips, nil
}
