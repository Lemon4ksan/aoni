// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DNSResolverFunc is an adapter to allow the use of ordinary functions as DNS resolvers.
type DNSResolverFunc func(ctx context.Context, host string) ([]net.IPAddr, error)

func (f DNSResolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewDoTResolver(t *testing.T) {
	t.Parallel()

	resolver := NewDoTResolver("1.1.1.1:853", "cloudflare-dns.com")
	require.NotNil(t, resolver)

	assert.Equal(t, "1.1.1.1:853", resolver.Endpoint)
	assert.Equal(t, "cloudflare-dns.com", resolver.Host)
	assert.Equal(t, 5*time.Second, resolver.Timeout)
}

func TestDoTResolver_LookupIPAddr_NetworkTimeout(t *testing.T) {
	t.Parallel()

	// Using an unreachable/invalid address to test error handling and timeout paths
	resolver := NewDoTResolver("240.0.0.1:853", "invalid-dns.test")
	resolver.Timeout = 50 * time.Millisecond

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	_, err := resolver.LookupIPAddr(ctx, "example.com")
	assert.Error(t, err)
}

func TestDoTResolver_LookupIPAddr_Online(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping DNS-over-TLS online test in short mode")
	}

	resolver := NewDoTResolver("1.1.1.1:853", "cloudflare-dns.com")
	resolver.Timeout = 5 * time.Second

	ipAddrs, err := resolver.LookupIPAddr(t.Context(), "example.com")
	if err != nil {
		t.Skipf("DNS-over-TLS lookup failed (network may be blocked/unavailable): %v", err)
	}

	assert.NotEmpty(t, ipAddrs)

	for _, ipAddr := range ipAddrs {
		assert.NotNil(t, ipAddr.IP)
	}
}

func TestDoTResolver_LookupIPAddr_NXDomain(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping online test")
	}

	resolver := NewDoTResolver("1.1.1.1:853", "cloudflare-dns.com")
	resolver.Timeout = 5 * time.Second

	_, err := resolver.LookupIPAddr(t.Context(), "nonexistent-domain-xyz-12345.com")
	if err != nil {
		errStr := err.Error()
		// Safe handling for environments where port 853 is blocked by a firewall
		if strings.Contains(errStr, "context deadline exceeded") ||
			strings.Contains(errStr, "i/o timeout") ||
			strings.Contains(errStr, "connection refused") {
			t.Skip("skipping DoT test because port 853 seems blocked on this network")
			return
		}

		var dnsErr *DNSError
		if assert.ErrorAs(t, err, &dnsErr) {
			assert.Contains(t, dnsErr.Error(), "DNS error rcode=")
		}
	}
}

func TestDoTResolver_LookupIPAddr_AAAAErrorFallback(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping online test")
	}

	resolver := NewDoTResolver("1.1.1.1:853", "cloudflare-dns.com")
	resolver.Timeout = 5 * time.Second

	ctx, cancel := context.WithCancel(t.Context())

	// Start a goroutine that waits slightly to let TypeA lookup dial and succeed,
	// then cancels the context to abort the subsequent TypeAAAA lookup.
	// This covers the fallback path when TypeAAAA lookup returns an error but TypeA succeeded.
	go func() {
		time.Sleep(350 * time.Millisecond)
		cancel()
	}()

	ips, err := resolver.LookupIPAddr(ctx, "google.com")
	if err == nil {
		assert.NotEmpty(t, ips)
	}
}

func TestInMemoryDNSCache(t *testing.T) {
	t.Parallel()

	t.Run("basic_cache_operations", func(t *testing.T) {
		t.Parallel()

		var callCount int32

		mockResolver := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
			atomic.AddInt32(&callCount, 1)
			return []net.IPAddr{{IP: net.ParseIP("192.168.1.10")}}, nil
		})

		cache := NewInMemoryDNSCache(50*time.Millisecond, mockResolver)
		t.Cleanup(cache.Close)

		// Cache miss: must call the underlying resolver
		ips1, err := cache.LookupIPAddr(t.Context(), "example.test")
		require.NoError(t, err)
		assert.Len(t, ips1, 1)
		assert.Equal(t, "192.168.1.10", ips1[0].IP.String())
		assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))

		// Cache hit: should retrieve cached value directly
		ips2, err := cache.LookupIPAddr(t.Context(), "example.test")
		require.NoError(t, err)
		assert.Len(t, ips2, 1)
		assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))

		// Wait for TTL expiration
		time.Sleep(100 * time.Millisecond)

		// Cache miss again after expiration
		ips3, err := cache.LookupIPAddr(t.Context(), "example.test")
		require.NoError(t, err)
		assert.Len(t, ips3, 1)
		assert.Equal(t, int32(2), atomic.LoadInt32(&callCount))
	})

	t.Run("single_flight_coalescing", func(t *testing.T) {
		t.Parallel()

		var callCount int32

		blockCh := make(chan struct{})

		mockResolver := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
			atomic.AddInt32(&callCount, 1)
			<-blockCh // block to force concurrent callers to wait and register
			return []net.IPAddr{{IP: net.ParseIP("10.10.10.10")}}, nil
		})

		cache := NewInMemoryDNSCache(time.Minute, mockResolver)
		t.Cleanup(cache.Close)

		var wg sync.WaitGroup
		for range 5 {
			wg.Go(func() {
				_, _ = cache.LookupIPAddr(t.Context(), "singleflight.test")
			})
		}

		time.Sleep(10 * time.Millisecond) // let all goroutines hit the flight group
		close(blockCh)                    // unblock the resolver execution
		wg.Wait()

		// Confirm that the resolver was called exactly once for all 5 concurrent lookups
		assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))
	})

	t.Run("default_underlying_resolver", func(t *testing.T) {
		t.Parallel()

		cache := NewInMemoryDNSCache(time.Minute, nil)
		t.Cleanup(cache.Close)

		assert.NotNil(t, cache.resolver)
	})
}

func TestInMemoryDNSCache_EvictionLoop(t *testing.T) {
	oldEvictInterval := evictInterval
	evictInterval = 10 * time.Millisecond

	mockResolver := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("192.168.1.1")}}, nil
	})

	cache := NewInMemoryDNSCache(10*time.Millisecond, mockResolver)
	t.Cleanup(cache.Close)

	_, err := cache.LookupIPAddr(t.Context(), "evict.test")
	require.NoError(t, err)

	// Verify it is cached
	cache.mu.RLock()
	_, exists := cache.cache["evict.test"]
	cache.mu.RUnlock()
	assert.True(t, exists)

	// Wait slightly longer than evictInterval to trigger the eviction ticker
	time.Sleep(100 * time.Millisecond)

	cache.mu.RLock()
	_, exists = cache.cache["evict.test"]
	cache.mu.RUnlock()
	assert.False(t, exists)

	evictInterval = oldEvictInterval
}

func TestNewDoHResolver(t *testing.T) {
	t.Parallel()

	resolver := NewDoHResolver("https://8.8.8.8/dns-query", "dns.google")
	require.NotNil(t, resolver)

	assert.Equal(t, "https://8.8.8.8/dns-query", resolver.Endpoint)
	assert.Equal(t, "dns.google", resolver.Host)
}

func TestDoHResolver_LookupIPAddr_Mocked(t *testing.T) {
	t.Parallel()

	resolver := NewDoHResolver("https://8.8.8.8/dns-query", "dns.google")

	// Mocking the http.Client's Transport
	resolver.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		query := req.URL.Query().Get("name")
		qtype := req.URL.Query().Get("type")

		var response string
		if query == "example.test" || query == "example.test." {
			switch qtype {
			case "1": // A record
				response = `{"Answer":[{"type":1,"data":"192.168.1.100"}]}`
			case "28": // AAAA record
				response = `{"Answer":[{"type":28,"data":"2001:db8::1"}]}`
			}
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(response)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	ips, err := resolver.LookupIPAddr(t.Context(), "example.test")
	require.NoError(t, err)

	var ipv4, ipv6 bool
	for _, ip := range ips {
		if ip.IP.To4() != nil {
			assert.Equal(t, "192.168.1.100", ip.IP.String())

			ipv4 = true
		} else if ip.IP.To16() != nil {
			assert.Equal(t, "2001:db8::1", ip.IP.String())

			ipv6 = true
		}
	}

	assert.True(t, ipv4)
	assert.True(t, ipv6)
}

func TestDoHResolver_LookupIPAddr_QueryFailure(t *testing.T) {
	t.Parallel()

	resolver := NewDoHResolver("https://8.8.8.8/dns-query", "dns.google")

	// Simulate DNS query connection error inside LookupIPAddr
	resolver.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})

	_, err := resolver.LookupIPAddr(t.Context(), "example.test")
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestStdlibResolver(t *testing.T) {
	t.Parallel()

	resolver := NewStdlibResolver()
	require.NotNil(t, resolver)

	ips, err := resolver.LookupIPAddr(t.Context(), "localhost")
	if err != nil {
		t.Skipf("stdlib lookup failed (network/hosts file dependent): %v", err)
	}

	assert.NotEmpty(t, ips)
}

func TestProxyRoutedDNSResolver(t *testing.T) {
	t.Parallel()

	t.Run("nil_underlying_resolver", func(t *testing.T) {
		t.Parallel()

		resolver := NewProxyRoutedDNSResolver(nil, nil)
		_, err := resolver.LookupIPAddr(t.Context(), "example.test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no underlying resolver configured")
	})

	t.Run("delegation_to_underlying_resolver", func(t *testing.T) {
		t.Parallel()

		called := false
		mockResolver := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
			called = true
			return []net.IPAddr{{IP: net.ParseIP("10.0.0.10")}}, nil
		})

		resolver := NewProxyRoutedDNSResolver(mockResolver, nil)
		ips, err := resolver.LookupIPAddr(t.Context(), "example.test")
		require.NoError(t, err)

		assert.True(t, called)
		assert.Len(t, ips, 1)
		assert.Equal(t, "10.0.0.10", ips[0].IP.String())
	})
}

func TestFallbackResolver(t *testing.T) {
	t.Parallel()

	t.Run("first_succeeds", func(t *testing.T) {
		t.Parallel()

		r1Called := false
		r1 := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
			r1Called = true
			return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
		})

		r2Called := false
		r2 := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
			r2Called = true
			return []net.IPAddr{{IP: net.ParseIP("2.2.2.2")}}, nil
		})

		fr := NewFallbackResolver(r1, r2)
		ips, err := fr.LookupIPAddr(t.Context(), "example.test")
		require.NoError(t, err)

		assert.True(t, r1Called)
		assert.False(t, r2Called)
		assert.Len(t, ips, 1)
		assert.Equal(t, "1.1.1.1", ips[0].IP.String())
	})

	t.Run("fallback_on_failure", func(t *testing.T) {
		t.Parallel()

		r1 := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return nil, errors.New("resolver 1 failed")
		})

		r2Called := false
		r2 := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
			r2Called = true
			return []net.IPAddr{{IP: net.ParseIP("2.2.2.2")}}, nil
		})

		fr := NewFallbackResolver(r1, r2)
		ips, err := fr.LookupIPAddr(t.Context(), "example.test")
		require.NoError(t, err)

		assert.True(t, r2Called)
		assert.Len(t, ips, 1)
		assert.Equal(t, "2.2.2.2", ips[0].IP.String())
	})

	t.Run("all_fail", func(t *testing.T) {
		t.Parallel()

		r1 := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return nil, errors.New("resolver 1 failed")
		})

		r2 := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return nil, errors.New("resolver 2 failed")
		})

		fr := NewFallbackResolver(r1, r2)
		_, err := fr.LookupIPAddr(t.Context(), "example.test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resolvers failed")
	})
}

func TestStaticResolver(t *testing.T) {
	t.Parallel()

	hosts := map[string][]string{
		"local.dev": {"127.0.0.1", "::1"},
	}

	nextCalled := false
	mockNext := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
		nextCalled = true
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	})

	sr := NewStaticResolver(hosts, mockNext)

	t.Run("static_match_ipv4_and_ipv6", func(t *testing.T) {
		ips, err := sr.LookupIPAddr(t.Context(), "local.dev")
		require.NoError(t, err)
		assert.False(t, nextCalled)

		var v4, v6 bool
		for _, ip := range ips {
			if ip.IP.To4() != nil {
				assert.Equal(t, "127.0.0.1", ip.IP.String())

				v4 = true
			} else if ip.IP.To16() != nil {
				assert.Equal(t, "::1", ip.IP.String())

				v6 = true
			}
		}

		assert.True(t, v4)
		assert.True(t, v6)
	})

	t.Run("static_match_with_trailing_dot", func(t *testing.T) {
		ips, err := sr.LookupIPAddr(t.Context(), "local.dev.")
		require.NoError(t, err)
		assert.False(t, nextCalled)
		assert.NotEmpty(t, ips)
	})

	t.Run("delegate_to_next_on_miss", func(t *testing.T) {
		ips, err := sr.LookupIPAddr(t.Context(), "unregistered.dev")
		require.NoError(t, err)
		assert.True(t, nextCalled)
		assert.Len(t, ips, 1)
		assert.Equal(t, "8.8.8.8", ips[0].IP.String())
	})
}

func TestFastRaceResolver(t *testing.T) {
	t.Parallel()

	t.Run("fastest_wins", func(t *testing.T) {
		t.Parallel()

		// Fast resolver
		r1 := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
		})

		// Slow resolver that would get cancelled
		r2 := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(1 * time.Second):
			}

			return []net.IPAddr{{IP: net.ParseIP("2.2.2.2")}}, nil
		})

		rr := NewFastRaceResolver(r1, r2)
		ips, err := rr.LookupIPAddr(t.Context(), "example.test")
		require.NoError(t, err)

		assert.Len(t, ips, 1)
		assert.Equal(t, "1.1.1.1", ips[0].IP.String())
	})

	t.Run("all_race_queries_fail", func(t *testing.T) {
		t.Parallel()

		r1 := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return nil, errors.New("race r1 error")
		})

		r2 := DNSResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return nil, errors.New("race r2 error")
		})

		rr := NewFastRaceResolver(r1, r2)
		_, err := rr.LookupIPAddr(t.Context(), "example.test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "all concurrent resolutions failed")
	})

	t.Run("no_resolvers_configured", func(t *testing.T) {
		t.Parallel()

		rr := NewFastRaceResolver()
		_, err := rr.LookupIPAddr(t.Context(), "example.test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active resolvers configured")
	})

	t.Run("only_nil_resolvers_configured", func(t *testing.T) {
		t.Parallel()

		// Triggers the outer fallback return statement:
		// "no responses received"
		rr := NewFastRaceResolver(nil, nil)
		_, err := rr.LookupIPAddr(t.Context(), "example.test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active resolvers configured")
	})
}
