// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/async/dedup"
	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/mach/proto/dns/wire"
	"github.com/lemon4ksan/foundation/silicon/clock"
)

// Standard DNS caching and resiliency constants defined in RFC 8767 and RFC 2308.
const (
	// MaxTTLCap is the maximum recommended authoritative TTL cap of 7 days (RFC 8767 §4).
	MaxTTLCap = 7 * 24 * time.Hour

	// DefaultMaxStaleTTL is the recommended retention duration for expired records in cache (RFC 8767 §5).
	DefaultMaxStaleTTL = 3 * 24 * time.Hour

	// DefaultStaleResponseTTL is the recommended TTL to set on stale records in responses (RFC 8767 §4 & §6).
	DefaultStaleResponseTTL = 30 * time.Second

	// DefaultClientResponseTimeout is the recommended time to wait for upstream before serving stale data (RFC 8767 §5).
	DefaultClientResponseTimeout = 1800 * time.Millisecond

	// FailureRecheckInterval is the recommended cooldown between retrying failed authoritative lookups (RFC 8767 §5).
	FailureRecheckInterval = 30 * time.Second

	// DefaultNegativeTTL is the default duration for caching NXDOMAIN and NODATA negative responses (RFC 2308 §5).
	DefaultNegativeTTL = 300 * time.Second

	// MaxNegativeTTLCap is the maximum recommended negative cache duration cap of 3 hours (RFC 2308 §5).
	MaxNegativeTTLCap = 3 * time.Hour
)

var evictInterval = time.Minute

type dnsCacheEntry struct {
	ips         []net.IPAddr
	negErr      error
	isNegative  bool
	freshUntil  time.Time
	staleUntil  time.Time
	lastFailure time.Time
}

// InMemoryDNSCache manages thread-safe, in-memory caching of DNS resolutions,
// strictly adhering to RFC 8767 (Serving Stale Data to Improve DNS Resiliency),
// RFC 2308 (Negative Caching of DNS Queries / DNS NCACHE),
// RFC 8020 (NXDOMAIN: There Really Is Nothing Underneath),
// and RFC 1035 authoritative TTLs.
type InMemoryDNSCache struct {
	cache                 generic.ConcurrentMap[string, dnsCacheEntry]
	ttl                   time.Duration
	negativeTTL           time.Duration
	maxStaleTTL           time.Duration
	clientResponseTimeout time.Duration
	serveStale            bool
	negativeCaching       bool
	resolver              Resolver
	sflight               dedup.Group[string, []net.IPAddr]
	cancel                context.CancelFunc
}

// CacheOption configures an [InMemoryDNSCache].
type CacheOption func(*InMemoryDNSCache)

// WithServeStale enables or disables serving stale DNS records on upstream resolution failures (RFC 8767).
func WithServeStale(enabled bool) CacheOption {
	return func(c *InMemoryDNSCache) {
		c.serveStale = enabled
	}
}

// WithNegativeCaching enables or disables negative response caching (RFC 2308).
func WithNegativeCaching(enabled bool) CacheOption {
	return func(c *InMemoryDNSCache) {
		c.negativeCaching = enabled
	}
}

// WithNegativeTTL configures the caching duration for negative responses (RFC 2308 §5).
func WithNegativeTTL(d time.Duration) CacheOption {
	return func(c *InMemoryDNSCache) {
		if d > MaxNegativeTTLCap {
			d = MaxNegativeTTLCap
		}
		c.negativeTTL = d
	}
}

// WithMaxStaleTTL configures the retention duration for expired records in cache (RFC 8767 §5).
func WithMaxStaleTTL(d time.Duration) CacheOption {
	return func(c *InMemoryDNSCache) {
		if d > MaxTTLCap {
			d = MaxTTLCap
		}
		c.maxStaleTTL = d
	}
}

// WithClientResponseTimeout sets the timeout before returning stale data while upstream lookup continues (RFC 8767 §5).
func WithClientResponseTimeout(d time.Duration) CacheOption {
	return func(c *InMemoryDNSCache) {
		c.clientResponseTimeout = d
	}
}

// NewInMemoryDNSCache creates an InMemoryDNSCache and launches background cache eviction.
func NewInMemoryDNSCache(ttl time.Duration, r Resolver, opts ...CacheOption) *InMemoryDNSCache {
	if r == nil {
		r = &net.Resolver{}
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &InMemoryDNSCache{
		ttl:                   ttl,
		negativeTTL:           DefaultNegativeTTL,
		maxStaleTTL:           DefaultMaxStaleTTL,
		clientResponseTimeout: DefaultClientResponseTimeout,
		serveStale:            true,
		negativeCaching:       true,
		resolver:              r,
		cancel:                cancel,
	}

	for _, opt := range opts {
		opt(c)
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
	now := clock.CoarseTime()
	c.cache.Range(func(k string, v dnsCacheEntry) bool {
		if now.After(v.staleUntil) {
			c.cache.Delete(k)
		}

		return true
	})
}

// checkAncestorNXDOMAIN checks if any parent ancestor domain of host has an active cached NXDOMAIN (RFC 8020).
func (c *InMemoryDNSCache) checkAncestorNXDOMAIN(host string, now time.Time) (error, bool) {
	idx := 0
	for {
		nextDot := strings.IndexByte(host[idx:], '.')
		if nextDot == -1 {
			break
		}
		idx += nextDot + 1
		if idx >= len(host) {
			break
		}
		ancestor := host[idx:]
		if entry, ok := c.cache.Load(ancestor); ok {
			if entry.isNegative && now.Before(entry.freshUntil) && IsNXDomain(entry.negErr) {
				return entry.negErr, true
			}
		}
	}
	return nil, false
}

// LookupIPAddr resolves host using cached TTL entries or queries the underlying resolver.
// Adheres to RFC 8767 (serve-stale resilience), RFC 2308 (negative caching), and RFC 8020 (NXDOMAIN cut).
func (c *InMemoryDNSCache) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	now := time.Now()
	entry, ok := c.cache.Load(host)
	if ok {
		// Fresh negative cache hit (RFC 2308 §5 & §8)
		if entry.isNegative && now.Before(entry.freshUntil) {
			return nil, entry.negErr
		}

		// Fresh positive cache hit (RFC 1035 §3.2.1)
		if !entry.isNegative && now.Before(entry.freshUntil) {
			return entry.ips, nil
		}

		// Fast-path for failure recheck window (RFC 8767 §5)
		if c.serveStale && !entry.isNegative && now.Before(entry.staleUntil) && !entry.lastFailure.IsZero() &&
			now.Sub(entry.lastFailure) < FailureRecheckInterval {
			return entry.ips, nil
		}
	}

	// Check ancestor NXDOMAIN cut: nonexistence of parent implies nonexistence of subtree (RFC 8020 §2 & §3.2)
	if c.negativeCaching {
		if ancErr, hasAnc := c.checkAncestorNXDOMAIN(host, now); hasAnc {
			return nil, ancErr
		}
	}

	ips, err := c.sflight.Do(ctx, host, func(ctx context.Context) ([]net.IPAddr, error) {
		currentNow := time.Now()
		cachedEntry, cachedOK := c.cache.Load(host)
		if cachedOK {
			if cachedEntry.isNegative && currentNow.Before(cachedEntry.freshUntil) {
				return nil, cachedEntry.negErr
			}
			if !cachedEntry.isNegative && currentNow.Before(cachedEntry.freshUntil) {
				return cachedEntry.ips, nil
			}
		}

		// Check ancestor NXDOMAIN cut within singleflight (RFC 8020)
		if c.negativeCaching {
			if ancErr, hasAnc := c.checkAncestorNXDOMAIN(host, currentNow); hasAnc {
				return nil, ancErr
			}
		}

		// Perform upstream lookup with client response timer (RFC 8767 §5)
		var cancel context.CancelFunc
		resolveCtx := ctx
		if c.serveStale && cachedOK && !cachedEntry.isNegative && currentNow.Before(cachedEntry.staleUntil) &&
			c.clientResponseTimeout > 0 {
			resolveCtx, cancel = context.WithTimeout(ctx, c.clientResponseTimeout)
		}
		if cancel != nil {
			defer cancel()
		}

		if extendedResolver, isExt := c.resolver.(interface {
			LookupDNSRecords(ctx context.Context, host string) ([]wire.DNSRecord, error)
		}); isExt {
			records, extErr := extendedResolver.LookupDNSRecords(resolveCtx, host)
			if extErr == nil && len(records) > 0 {
				return c.storeRecords(host, records)
			}
			if extErr != nil && c.serveStale && cachedOK && !cachedEntry.isNegative &&
				currentNow.Before(cachedEntry.staleUntil) {
				cachedEntry.lastFailure = currentNow
				c.cache.Store(host, cachedEntry)
				return cachedEntry.ips, nil
			}
		}

		resolvedIPs, resolveErr := c.resolver.LookupIPAddr(resolveCtx, host)
		if resolveErr != nil {
			// RFC 2308 §5 & RFC 8020 §2: Cache authoritative negative responses (NXDOMAIN / NODATA)
			if c.negativeCaching && IsNotFound(resolveErr) {
				negTTL := c.negativeTTL
				if negTTL <= 0 {
					negTTL = DefaultNegativeTTL
				}
				negFreshUntil := currentNow.Add(negTTL)
				c.cache.Store(host, dnsCacheEntry{
					isNegative: true,
					negErr:     resolveErr,
					freshUntil: negFreshUntil,
					staleUntil: negFreshUntil,
				})

				return nil, resolveErr
			}

			// RFC 8767 §4 & §5: If upstream resolution fails, fallback to stale data
			if c.serveStale && cachedOK && !cachedEntry.isNegative && currentNow.Before(cachedEntry.staleUntil) {
				cachedEntry.lastFailure = currentNow
				c.cache.Store(host, cachedEntry)
				return cachedEntry.ips, nil
			}

			return nil, resolveErr
		}

		effectiveTTL := c.ttl
		if effectiveTTL <= 0 {
			effectiveTTL = DefaultStaleResponseTTL
		}

		freshUntil := currentNow.Add(effectiveTTL)
		staleUntil := freshUntil.Add(c.maxStaleTTL)

		c.cache.Store(host, dnsCacheEntry{
			ips:        resolvedIPs,
			freshUntil: freshUntil,
			staleUntil: staleUntil,
		})

		return resolvedIPs, nil
	})
	if err != nil {
		return nil, WrapDNSError(host, "InMemoryCache", "", err)
	}

	return ips, nil
}

func (c *InMemoryDNSCache) storeRecords(host string, records []wire.DNSRecord) ([]net.IPAddr, error) {
	if len(records) == 0 {
		return nil, errors.New("empty dns records")
	}

	ips := make([]net.IPAddr, len(records))
	ttls := make([]uint32, len(records))

	for i, r := range records {
		ips[i] = net.IPAddr{IP: r.Addr.AsSlice()}
		ttls[i] = r.TTL
	}

	// Select lowest TTL in RRSet per RFC 2181 §5.2 and clamp per RFC 8767 §4
	effectiveTTL := max(wire.SelectRRSetTTL(ttls...), 5*time.Second)

	now := time.Now()
	freshUntil := now.Add(effectiveTTL)
	staleUntil := freshUntil.Add(c.maxStaleTTL)

	c.cache.Store(host, dnsCacheEntry{
		ips:        ips,
		freshUntil: freshUntil,
		staleUntil: staleUntil,
	})

	return ips, nil
}
