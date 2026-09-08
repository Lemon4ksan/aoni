// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/dns"
)

type mockResolver struct {
	ips []net.IPAddr
	err error
}

func (m *mockResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	if m.err != nil {
		return nil, m.err
	}

	return m.ips, nil
}

func TestResolversAndLookups(t *testing.T) {
	t.Parallel()

	ip1 := net.ParseIP("192.0.2.1")
	ip2 := net.ParseIP("192.0.2.2")
	mock := &mockResolver{
		ips: []net.IPAddr{
			{IP: ip1},
			{IP: ip2},
		},
	}

	t.Run("lookup_first_ip_and_result", func(t *testing.T) {
		t.Parallel()

		first := dns.LookupFirstIP(t.Context(), mock, "example.com")
		require.True(t, first.IsPresent())
		val, ok := first.Value()
		require.True(t, ok)
		assert.Equal(t, ip1.String(), val.String())

		res := dns.LookupIPAddrResult(t.Context(), mock, "example.com")
		assert.True(t, res.IsSuccess())
		addrs, err := res.Unwrap()
		require.NoError(t, err)
		assert.Len(t, addrs, 2)
	})

	t.Run("lookup_netip_helpers", func(t *testing.T) {
		t.Parallel()

		netIPs, err := dns.LookupNetIP(t.Context(), mock, "example.com")
		require.NoError(t, err)
		assert.Len(t, netIPs, 2)

		res := dns.LookupNetIPResult(t.Context(), mock, "example.com")
		assert.True(t, res.IsSuccess())

		firstNet := dns.LookupFirstNetIP(t.Context(), mock, "example.com")
		require.True(t, firstNet.IsPresent())
		fnVal, ok := firstNet.Value()
		require.True(t, ok)
		assert.Equal(t, ip1.String(), fnVal.String())
	})

	t.Run("static_resolver", func(t *testing.T) {
		t.Parallel()

		mapping := map[string][]string{
			"custom.local": {"10.0.0.1", "10.0.0.2"},
		}
		staticRes := dns.NewStaticResolver(mapping, mock)
		require.NotNil(t, staticRes)

		addrs, err := staticRes.LookupIPAddr(t.Context(), "custom.local")
		require.NoError(t, err)
		assert.Len(t, addrs, 2)
		assert.Equal(t, "10.0.0.1", addrs[0].IP.String())

		// Fallback to delegate for unmapped domains
		delegateAddrs, err := staticRes.LookupIPAddr(t.Context(), "fallback.com")
		require.NoError(t, err)
		assert.Len(t, delegateAddrs, 2)
	})

	t.Run("fallback_resolver", func(t *testing.T) {
		t.Parallel()

		errRes := &mockResolver{err: errors.New("lookup failed")}
		fb := dns.NewFallbackResolver(errRes, mock)
		require.NotNil(t, fb)

		addrs, err := fb.LookupIPAddr(t.Context(), "example.com")
		require.NoError(t, err)
		assert.Len(t, addrs, 2)
	})

	t.Run("fast_race_resolver", func(t *testing.T) {
		t.Parallel()

		race := dns.NewFastRaceResolver(mock)
		require.NotNil(t, race)

		addrs, err := race.LookupIPAddr(t.Context(), "example.com")
		require.NoError(t, err)
		assert.Len(t, addrs, 2)
	})

	t.Run("stdlib_and_dot_and_proxy_routed_constructors", func(t *testing.T) {
		t.Parallel()

		std := dns.NewStdlibResolver()
		assert.NotNil(t, std)

		dot := dns.NewDoTResolver("1.1.1.1:853", "cloudflare-dns.com")
		assert.NotNil(t, dot)

		proxyRouted := dns.NewProxyRoutedDNSResolver(mock, func(_ context.Context, _, _ string) (net.Conn, error) {
			s, c := net.Pipe()
			_ = s.Close()
			return c, nil
		})
		assert.NotNil(t, proxyRouted)
	})

	t.Run("in_memory_dns_cache_options", func(t *testing.T) {
		t.Parallel()

		cache := dns.NewInMemoryDNSCache(
			time.Minute,
			mock,
			dns.WithServeStale(true),
			dns.WithNegativeCaching(true),
			dns.WithNegativeTTL(10*time.Second),
			dns.WithMaxStaleTTL(time.Hour),
			dns.WithClientResponseTimeout(500*time.Millisecond),
		)
		require.NotNil(t, cache)

		addrs, err := cache.LookupIPAddr(t.Context(), "example.com")
		require.NoError(t, err)
		assert.Len(t, addrs, 2)
	})

	t.Run("error_predicates", func(t *testing.T) {
		t.Parallel()

		assert.False(t, dns.IsNotFound(nil))
		assert.False(t, dns.IsNXDomain(nil))
		assert.False(t, dns.IsNotFound(errors.New("generic error")))
		assert.False(t, dns.IsNXDomain(errors.New("generic error")))

		assert.True(t, dns.IsNXDomain(dns.ErrNXDomain))
		assert.True(t, dns.IsNotFound(dns.ErrNXDomain))
		assert.True(t, dns.IsNotFound(dns.ErrNODATA))
	})
}
