// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/silicon/clock"
)

// StaleOptions configures the behavior of [StaleResolver] based on Chromium's StaleHostResolver (RFC 8767).
type StaleOptions struct {
	// MaxExpiredTime specifies how long stale records remain usable past expiration.
	// Defaults to 24 hours (RFC 8767 §5).
	MaxExpiredTime time.Duration

	// StaleDelay specifies the grace duration to wait for upstream resolution before returning stale records.
	// A delay of 0s (the default) returns stale records immediately for 0ms latency while refreshing in background.
	StaleDelay time.Duration

	// DefaultTTL specifies the TTL assigned to freshly resolved records if none is provided.
	// Defaults to 5 minutes.
	DefaultTTL time.Duration
}

type staleEntry struct {
	addrs     []net.IPAddr
	expiresAt time.Time
	fetchedAt time.Time
}

func (e staleEntry) isFresh(now time.Time) bool {
	return now.Before(e.expiresAt)
}

func (e staleEntry) isUsableStale(now time.Time, maxExpired time.Duration) bool {
	if maxExpired <= 0 {
		return true
	}

	return now.Sub(e.expiresAt) <= maxExpired
}

// StaleResolver wraps any [Resolver] to implement Chromium-grade Stale DNS Resolution (RFC 8767).
// When cached records expire, it immediately serves stale IP addresses to avoid DNS latency stalls,
// while asynchronously refreshing the cache in the background.
//
// Thread Safety & Concurrency:
// 100% thread-safe for concurrent read and write operations. Background refresh operations
// are deduplicated per hostname to prevent upstream DNS stampedes.
type StaleResolver struct {
	delegate Resolver
	options  StaleOptions

	cache    generic.ConcurrentMap[string, staleEntry]
	inflight sync.Map
}

// NewStaleResolver instantiates a [StaleResolver] wrapping delegate with the provided options.
func NewStaleResolver(delegate Resolver, opts ...func(*StaleOptions)) *StaleResolver {
	opt := StaleOptions{
		MaxExpiredTime: 24 * time.Hour,
		StaleDelay:     0,
		DefaultTTL:     5 * time.Minute,
	}

	for _, fn := range opts {
		if fn != nil {
			fn(&opt)
		}
	}

	return &StaleResolver{
		delegate: delegate,
		options:  opt,
	}
}

// LookupIPAddr satisfies the [Resolver] interface.
func (s *StaleResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if host == "" {
		return nil, nil
	}

	// 1. Direct IP check
	if ip := net.ParseIP(host); ip != nil {
		return []net.IPAddr{{IP: ip}}, nil
	}

	now := clock.CoarseTime()

	// 2. Cache hit evaluation
	if entry, ok := s.cache.Load(host); ok {
		if entry.isFresh(now) {
			return entry.addrs, nil
		}

		if entry.isUsableStale(now, s.options.MaxExpiredTime) {
			if s.options.StaleDelay == 0 {
				s.triggerBackgroundRefresh(host)

				return entry.addrs, nil
			}

			// Wait up to StaleDelay for fresh upstream results before serving stale data
			freshAddrs, err := s.lookupWithTimeout(ctx, host, s.options.StaleDelay)
			if err == nil && len(freshAddrs) > 0 {
				return freshAddrs, nil
			}

			s.triggerBackgroundRefresh(host)

			return entry.addrs, nil
		}
	}

	// 3. Cache miss or too stale: synchronous lookup
	return s.fetchAndCache(ctx, host)
}

// LookupNetIP resolves a hostname into a slice of [netip.Addr].
func (s *StaleResolver) LookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	addrs, err := s.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	netAddrs := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		if na, ok := netip.AddrFromSlice(a.IP); ok {
			netAddrs = append(netAddrs, na.Unmap())
		}
	}

	return netAddrs, nil
}

func (s *StaleResolver) lookupWithTimeout(
	ctx context.Context,
	host string,
	timeout time.Duration,
) ([]net.IPAddr, error) {
	tCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return s.delegate.LookupIPAddr(tCtx, host)
}

func (s *StaleResolver) fetchAndCache(ctx context.Context, host string) ([]net.IPAddr, error) {
	addrs, err := s.delegate.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	if len(addrs) > 0 {
		now := clock.CoarseTime()
		s.cache.Store(host, staleEntry{
			addrs:     addrs,
			expiresAt: now.Add(s.options.DefaultTTL),
			fetchedAt: now,
		})
	}

	return addrs, nil
}

func (s *StaleResolver) triggerBackgroundRefresh(host string) {
	if _, loaded := s.inflight.LoadOrStore(host, struct{}{}); loaded {
		return
	}

	go func() {
		defer s.inflight.Delete(host)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		addrs, err := s.delegate.LookupIPAddr(ctx, host)
		if err == nil && len(addrs) > 0 {
			now := clock.CoarseTime()
			s.cache.Store(host, staleEntry{
				addrs:     addrs,
				expiresAt: now.Add(s.options.DefaultTTL),
				fetchedAt: now,
			})
		}
	}()
}
