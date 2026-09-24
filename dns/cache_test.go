// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"
	"github.com/lemon4ksan/mach/proto/dns/wire"

	"github.com/lemon4ksan/aoni/dns"
)

type counterResolver struct {
	calls atomic.Int32
	ips   []net.IPAddr
	err   error
}

func (c *counterResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	c.calls.Add(1)

	if c.err != nil {
		return nil, c.err
	}

	return c.ips, nil
}

func TestCache_PositiveHitAndTTL(t *testing.T) {
	t.Parallel()

	res := &counterResolver{
		ips: []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}},
	}

	cache := dns.NewInMemoryDNSCache(50*time.Millisecond, res)
	defer cache.Close()

	// Initial lookup
	ips, err := cache.LookupIPAddr(context.Background(), "example.com")
	require.NoError(t, err)
	assert.Len(t, ips, 1)
	assert.Equal(t, int32(1), res.calls.Load())

	// Cache hit
	ips2, err2 := cache.LookupIPAddr(context.Background(), "example.com")
	require.NoError(t, err2)
	assert.Equal(t, ips, ips2)
	assert.Equal(t, int32(1), res.calls.Load())

	// Expire TTL
	time.Sleep(60 * time.Millisecond)

	// Refreshed lookup
	ips3, err3 := cache.LookupIPAddr(context.Background(), "example.com")
	require.NoError(t, err3)
	assert.Equal(t, ips, ips3)
	assert.Equal(t, int32(2), res.calls.Load())
}

func TestCache_NegativeCaching_RFC2308(t *testing.T) {
	t.Parallel()

	res := &counterResolver{
		err: dns.ErrNXDomain,
	}

	cache := dns.NewInMemoryDNSCache(time.Minute, res,
		dns.WithNegativeCaching(true),
		dns.WithNegativeTTL(50*time.Millisecond),
	)
	defer cache.Close()

	// First query -> NXDOMAIN
	_, err := cache.LookupIPAddr(context.Background(), "nonexistent.com")
	assert.ErrorIs(t, err, dns.ErrNXDomain)
	assert.Equal(t, int32(1), res.calls.Load())

	// Second query -> cached NXDOMAIN without calling upstream
	_, err2 := cache.LookupIPAddr(context.Background(), "nonexistent.com")
	assert.ErrorIs(t, err2, dns.ErrNXDomain)
	assert.Equal(t, int32(1), res.calls.Load())
}

func TestCache_AncestorNXDOMAIN_RFC8020(t *testing.T) {
	t.Parallel()

	res := &counterResolver{
		err: dns.ErrNXDomain,
	}

	cache := dns.NewInMemoryDNSCache(time.Minute, res,
		dns.WithNegativeCaching(true),
		dns.WithNegativeTTL(time.Minute),
	)
	defer cache.Close()

	// Query parent -> cached NXDOMAIN
	_, err := cache.LookupIPAddr(context.Background(), "nonexistent.com")
	assert.ErrorIs(t, err, dns.ErrNXDomain)
	assert.Equal(t, int32(1), res.calls.Load())

	// Query child subdomain -> immediately returns ancestor NXDOMAIN without calling upstream
	_, errChild := cache.LookupIPAddr(context.Background(), "sub.nonexistent.com")
	assert.ErrorIs(t, errChild, dns.ErrNXDomain)
	assert.Equal(t, int32(1), res.calls.Load())
}

func TestCache_ServeStale_RFC8767(t *testing.T) {
	t.Parallel()

	var failUpstream atomic.Bool

	res := dns.ResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
		if failUpstream.Load() {
			return nil, errors.New("upstream timeout")
		}

		return []net.IPAddr{{IP: net.ParseIP("1.2.3.4")}}, nil
	})

	cache := dns.NewInMemoryDNSCache(20*time.Millisecond, res,
		dns.WithServeStale(true),
		dns.WithMaxStaleTTL(time.Hour),
		dns.WithClientResponseTimeout(10*time.Millisecond),
	)
	defer cache.Close()

	// Populate cache
	ips, err := cache.LookupIPAddr(context.Background(), "stale.example.com")
	require.NoError(t, err)
	assert.Equal(t, "1.2.3.4", ips[0].IP.String())

	// Make fresh TTL expire
	time.Sleep(30 * time.Millisecond)

	// Cause upstream failure
	failUpstream.Store(true)

	// Should serve stale cached record
	staleIPs, errStale := cache.LookupIPAddr(context.Background(), "stale.example.com")
	require.NoError(t, errStale)
	assert.Equal(t, "1.2.3.4", staleIPs[0].IP.String())
}

type recordResolver struct {
	records []wire.DNSRecord
}

func (r *recordResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return nil, errors.New("fallback")
}

func (r *recordResolver) LookupDNSRecords(ctx context.Context, host string) ([]wire.DNSRecord, error) {
	return r.records, nil
}

func TestCache_StoreDNSRecords(t *testing.T) {
	t.Parallel()

	recRes := &recordResolver{
		records: []wire.DNSRecord{
			{Addr: netip.MustParseAddr("10.0.0.1"), TTL: 300},
		},
	}

	cache := dns.NewInMemoryDNSCache(time.Minute, recRes)
	defer cache.Close()

	ips, err := cache.LookupIPAddr(context.Background(), "example.com")
	require.NoError(t, err)
	assert.Len(t, ips, 1)
	assert.Equal(t, "10.0.0.1", ips[0].IP.String())
}
