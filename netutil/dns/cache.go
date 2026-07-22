// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/batto"
)

var evictInterval = time.Minute

type dnsCacheEntry struct {
	ips    []net.IPAddr
	expiry time.Time
}

// InMemoryDNSCache caches DNS results in memory for the configured TTL.
type InMemoryDNSCache struct {
	mu       sync.RWMutex
	cache    map[string]dnsCacheEntry
	ttl      time.Duration
	resolver Resolver
	sflight  batto.Group[string, []net.IPAddr]
	cancel   context.CancelFunc
}

// NewInMemoryDNSCache creates a new [InMemoryDNSCache] with the given TTL and resolver.
// A background goroutine periodically evicts expired entries.
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

// Close stops the background eviction goroutine.
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
			c.mu.Lock()

			now := time.Now()
			for k, v := range c.cache {
				if now.After(v.expiry) {
					delete(c.cache, k)
				}
			}

			c.mu.Unlock()
		}
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

	ips, err := c.sflight.Do(ctx, host, func(ctx context.Context) ([]net.IPAddr, error) {
		return c.resolver.LookupIPAddr(ctx, host)
	})
	if err != nil {
		return nil, wrapDNSError(host, "InMemoryCache", "", err)
	}

	c.mu.Lock()
	c.cache[host] = dnsCacheEntry{
		ips:    ips,
		expiry: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()

	return ips, nil
}
