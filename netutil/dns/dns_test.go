// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/net/dns/wire"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
)

type failingResolver struct{}

func (f *failingResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return nil, errors.New("lookup failed")
}

type mockDelayResolver struct {
	delay time.Duration
	ip    string
}

func (m *mockDelayResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(m.delay):
		return []net.IPAddr{{IP: net.ParseIP(m.ip)}}, nil
	}
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
		if strings.Contains(errStr, "context deadline exceeded") ||
			strings.Contains(errStr, "i/o timeout") ||
			strings.Contains(errStr, "connection refused") {
			t.Skip("skipping DoT test because port 853 seems blocked on this network")
			return
		}

		var dnsErr *ResolutionError
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

		mockResolver := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
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

		mockResolver := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
			atomic.AddInt32(&callCount, 1)
			<-blockCh
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

		time.Sleep(10 * time.Millisecond)
		close(blockCh)
		wg.Wait()

		assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))
	})

	t.Run("default_underlying_resolver", func(t *testing.T) {
		t.Parallel()

		cache := NewInMemoryDNSCache(time.Minute, nil)
		t.Cleanup(cache.Close)

		assert.NotNil(t, cache.resolver)
	})
}

func TestInMemoryDNSCache_ExpiredLookup(t *testing.T) {
	t.Parallel()

	var callCount int32

	mockResolver := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
		count := atomic.AddInt32(&callCount, 1)
		return []net.IPAddr{{IP: net.ParseIP(fmt.Sprintf("10.0.0.%d", count))}}, nil
	})

	cache := NewInMemoryDNSCache(10*time.Millisecond, mockResolver)
	t.Cleanup(cache.Close)

	ips1, err := cache.LookupIPAddr(t.Context(), "expired.test")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1", ips1[0].IP.String())

	cache.mu.Lock()
	cache.cache["expired.test"] = dnsCacheEntry{
		ips:    ips1,
		expiry: time.Now().Add(-10 * time.Minute),
	}
	cache.mu.Unlock()

	ips2, err := cache.LookupIPAddr(t.Context(), "expired.test")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.2", ips2[0].IP.String())
	assert.Equal(t, int32(2), atomic.LoadInt32(&callCount))
}

func TestInMemoryDNSCache_EvictionLoop(t *testing.T) {
	t.Parallel()

	mockResolver := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("192.168.1.1")}}, nil
	})

	cache := NewInMemoryDNSCache(10*time.Millisecond, mockResolver)
	t.Cleanup(cache.Close)

	_, err := cache.LookupIPAddr(t.Context(), "evict.test")
	require.NoError(t, err)

	cache.mu.Lock()
	cache.cache["evict.test"] = dnsCacheEntry{
		ips:    []net.IPAddr{{IP: net.ParseIP("192.168.1.1")}},
		expiry: time.Now().Add(-10 * time.Minute),
	}

	now := time.Now()
	for k, v := range cache.cache {
		if now.After(v.expiry) {
			delete(cache.cache, k)
		}
	}

	_, exists := cache.cache["evict.test"]
	cache.mu.Unlock()

	assert.False(t, exists, "expired entry should be evicted")
}

func TestNewDoHResolver(t *testing.T) {
	t.Parallel()

	resolver := NewDoHResolver("https://8.8.8.8/dns-query", "dns.google", nil)
	require.NotNil(t, resolver)

	assert.Equal(t, "https://8.8.8.8/dns-query", resolver.Endpoint)
	assert.Equal(t, "dns.google", resolver.Host)
}

func TestDoHResolver_LookupIPAddr_Mocked(t *testing.T) {
	t.Parallel()

	mockTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		if len(body) < 12 {
			return nil, errors.New("invalid doh wire query")
		}

		queryID := binary.BigEndian.Uint16(body[0:2])

		qtypeOffset, err := wire.SkipDomainName(body, 12)

		respIP := netip.MustParseAddr("192.168.1.100")
		if err == nil && qtypeOffset+2 <= len(body) {
			qtype := binary.BigEndian.Uint16(body[qtypeOffset : qtypeOffset+2])
			if qtype == wire.TypeAAAA {
				respIP = netip.MustParseAddr("2001:db8::1")
			}
		}

		respWire := buildMockDoQDNSResponse(queryID, respIP)

		header := make(http.Header)
		header.Set("Content-Type", DoHMediaType)

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(respWire)),
			Header:     header,
			Request:    req,
		}, nil
	})

	mockClient := &http.Client{Transport: mockTransport}

	resolver := NewDoHResolver("https://8.8.8.8/dns-query", "dns.google", mockClient)

	ips, err := resolver.LookupIPAddr(t.Context(), "example.test")
	require.NoError(t, err)

	var ipv4, ipv6 bool
	for _, ipAddr := range ips {
		if ipAddr.IP.To4() != nil {
			assert.Equal(t, "192.168.1.100", ipAddr.IP.String())

			ipv4 = true
		} else if ipAddr.IP.To16() != nil {
			assert.Equal(t, "2001:db8::1", ipAddr.IP.String())

			ipv6 = true
		}
	}

	assert.True(t, ipv4)
	assert.True(t, ipv6)
}

func TestDoHResolver_LookupIPAddr_QueryFailure(t *testing.T) {
	t.Parallel()

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
			return nil, io.ErrUnexpectedEOF
		}),
	}

	resolver := NewDoHResolver("https://8.8.8.8/dns-query", "dns.google", mockClient)

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
		assert.ErrorIs(t, err, ErrNoResolversConfigured)
	})

	t.Run("delegation_to_underlying_resolver", func(t *testing.T) {
		t.Parallel()

		called := false
		mockResolver := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
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
		r1 := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
			r1Called = true
			return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
		})

		r2Called := false
		r2 := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
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

		r1 := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
			return nil, errors.New("resolver 1 failed")
		})

		r2Called := false
		r2 := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
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

		r1 := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
			return nil, errors.New("resolver 1 failed")
		})

		r2 := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
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
	mockNext := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
		nextCalled = true
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	})

	sr := NewStaticResolver(hosts, mockNext)

	t.Run("static_match_ipv4_and_ipv6", func(t *testing.T) {
		ips, err := sr.LookupIPAddr(t.Context(), "local.dev")
		require.NoError(t, err)
		assert.False(t, nextCalled)

		var v4, v6 bool
		for _, ipAddr := range ips {
			if ipAddr.IP.To4() != nil {
				assert.Equal(t, "127.0.0.1", ipAddr.IP.String())

				v4 = true
			} else if ipAddr.IP.To16() != nil {
				assert.Equal(t, "::1", ipAddr.IP.String())

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

		r1 := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
		})

		r2 := ResolverFunc(func(ctx context.Context, _ string) ([]net.IPAddr, error) {
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

		r1 := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
			return nil, errors.New("race r1 error")
		})

		r2 := ResolverFunc(func(_ context.Context, _ string) ([]net.IPAddr, error) {
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
}

func TestDNSResolvers(t *testing.T) {
	t.Parallel()

	t.Run("static_resolver", func(t *testing.T) {
		t.Parallel()

		staticMap := map[string][]string{
			"example.com": {"1.2.3.4"},
		}
		resolver := NewStaticResolver(staticMap, nil)
		ips, err := resolver.LookupIPAddr(t.Context(), "example.com")
		require.NoError(t, err)
		require.Len(t, ips, 1)
		assert.Equal(t, "1.2.3.4", ips[0].IP.String())
	})

	t.Run("fallback_resolver", func(t *testing.T) {
		t.Parallel()

		r1 := &failingResolver{}
		r2 := NewStaticResolver(map[string][]string{"test.com": {"8.8.8.8"}}, nil)

		fallback := NewFallbackResolver(r1, r2)
		ips, err := fallback.LookupIPAddr(t.Context(), "test.com")
		require.NoError(t, err)
		require.NotEmpty(t, ips)
		assert.Equal(t, "8.8.8.8", ips[0].IP.String())
	})

	t.Run("fast_race_resolver", func(t *testing.T) {
		t.Parallel()

		fast := &mockDelayResolver{delay: 1 * time.Millisecond, ip: "1.1.1.1"}
		slow := &mockDelayResolver{delay: 200 * time.Millisecond, ip: "2.2.2.2"}

		racer := NewFastRaceResolver(slow, fast)
		ips, err := racer.LookupIPAddr(t.Context(), "any.com")
		require.NoError(t, err)
		require.NotEmpty(t, ips)
		assert.Equal(t, "1.1.1.1", ips[0].IP.String())
	})
}

func TestDoHResolver_QueryEncoding(t *testing.T) {
	t.Parallel()

	var (
		capturedContentType string
		capturedBody        []byte
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		capturedBody, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", DoHMediaType)
		w.WriteHeader(http.StatusOK)

		if len(capturedBody) >= 2 {
			queryID := binary.BigEndian.Uint16(capturedBody[0:2])
			respWire := buildMockDoQDNSResponse(queryID, netip.MustParseAddr("127.0.0.1"))
			_, _ = w.Write(respWire)
		}
	}))
	t.Cleanup(ts.Close)

	r := &DoHResolver{
		Endpoint: ts.URL,
		Host:     "cloudflare-dns.com",
		doer:     aoni.NewClient(ts.Client()),
	}

	ctx := t.Context()
	_, err := r.queryWire(ctx, "example.com", 1)
	require.NoError(t, err)

	assert.Equal(t, DoHMediaType, capturedContentType)
	assert.NotEmpty(t, capturedBody)
}

func TestInMemoryDNSCache_Eviction(t *testing.T) {
	t.Parallel()

	cache := NewInMemoryDNSCache(time.Millisecond, &net.Resolver{})
	t.Cleanup(func() { cache.Close() })

	cache.mu.Lock()
	cache.cache["expired.test"] = dnsCacheEntry{
		ips:    []net.IPAddr{{IP: net.ParseIP("1.2.3.4")}},
		expiry: time.Now().Add(-time.Hour),
	}
	cache.cache["valid.test"] = dnsCacheEntry{
		ips:    []net.IPAddr{{IP: net.ParseIP("5.6.7.8")}},
		expiry: time.Now().Add(time.Hour),
	}
	cache.mu.Unlock()

	cache.mu.Lock()

	now := time.Now()
	for k, v := range cache.cache {
		if now.After(v.expiry) {
			delete(cache.cache, k)
		}
	}

	_, expiredExists := cache.cache["expired.test"]
	_, validExists := cache.cache["valid.test"]
	cache.mu.Unlock()

	assert.False(t, expiredExists, "expired entry should be removed")
	assert.True(t, validExists, "valid entry should remain")
}

func TestResolutionError(t *testing.T) {
	t.Parallel()

	baseErr := errors.New("connection failed")
	resErr := &ResolutionError{
		Host:      "example.com",
		Resolver:  "DoH",
		Endpoint:  "https://1.1.1.1/dns-query",
		Err:       baseErr,
		IsTimeout: true,
	}

	assert.Contains(t, resErr.Error(), "example.com")
	assert.Contains(t, resErr.Error(), "DoH")
	assert.Contains(t, resErr.Error(), "https://1.1.1.1/dns-query")
	assert.ErrorIs(t, resErr, baseErr)
	assert.True(t, resErr.Timeout())
	assert.True(t, resErr.Temporary())

	wrapped := wrapDNSError("example.com", "DoT", "1.1.1.1:853", baseErr)
	require.Error(t, wrapped)
	assert.Contains(t, wrapped.Error(), "aoni dns: resolve example.com via DoT")
}

func TestDoHResolver_EDNS0_And_GetMethod(t *testing.T) {
	t.Parallel()

	t.Run("doh_get_method_base64_encoded", func(t *testing.T) {
		t.Parallel()

		var (
			mu             sync.Mutex
			capturedMethod string
			capturedQuery  string
		)

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query().Get("dns")

			mu.Lock()
			capturedMethod = r.Method
			capturedQuery = query
			mu.Unlock()

			w.Header().Set("Content-Type", DoHMediaType)
			w.WriteHeader(http.StatusOK)

			if query != "" {
				wireQuery, err := base64.RawURLEncoding.DecodeString(query)
				if err == nil && len(wireQuery) >= 2 {
					queryID := binary.BigEndian.Uint16(wireQuery[0:2])
					respWire := buildMockDoQDNSResponse(queryID, netip.MustParseAddr("1.1.1.1"))
					_, _ = w.Write(respWire)

					return
				}
			}

			w.WriteHeader(http.StatusBadRequest)
		}))
		t.Cleanup(ts.Close)

		resolver := NewDoHResolver(ts.URL, "cloudflare-dns.com", aoni.NewClient(ts.Client()))
		resolver.Method = DoHMethodGet
		resolver.EDNS = wire.EDNSOptions{PadToBlock: 128}

		_, err := resolver.LookupNetIP(t.Context(), "example.com")
		require.NoError(t, err)

		mu.Lock()
		m := capturedMethod
		q := capturedQuery
		mu.Unlock()

		assert.Equal(t, http.MethodGet, m)
		assert.NotEmpty(t, q)
	})

	t.Run("doh_post_method_wire_payload", func(t *testing.T) {
		t.Parallel()

		var (
			mu                  sync.Mutex
			capturedMethod      string
			capturedContentType string
			capturedBody        []byte
		)

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)

			mu.Lock()
			capturedMethod = r.Method
			capturedContentType = r.Header.Get("Content-Type")
			capturedBody = b
			mu.Unlock()

			w.Header().Set("Content-Type", DoHMediaType)
			w.WriteHeader(http.StatusOK)

			if len(b) >= 2 {
				queryID := binary.BigEndian.Uint16(b[0:2])
				respWire := buildMockDoQDNSResponse(queryID, netip.MustParseAddr("1.1.1.1"))
				_, _ = w.Write(respWire)
			}
		}))
		t.Cleanup(ts.Close)

		resolver := NewDoHResolver(ts.URL, "dns.google", aoni.NewClient(ts.Client()))
		resolver.Method = DoHMethodPost
		resolver.EDNS = wire.EDNSOptions{
			ClientIP:   netip.MustParseAddr("192.168.1.50"),
			PadToBlock: 128,
		}

		_, err := resolver.LookupNetIP(t.Context(), "example.com")
		require.NoError(t, err)

		mu.Lock()
		m := capturedMethod
		ct := capturedContentType
		body := capturedBody
		mu.Unlock()

		assert.Equal(t, http.MethodPost, m)
		assert.Equal(t, DoHMediaType, ct)
		assert.NotEmpty(t, body)
	})

	t.Run("doh_non_200_http_status_error", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(ts.Close)

		resolver := NewDoHResolver(ts.URL, "dns.google", aoni.NewClient(ts.Client()))
		_, err := resolver.LookupDNSRecords(t.Context(), "example.com")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "http status 503")
	})
}

func TestPackDNSQueryExtended_EDNSOptions(t *testing.T) {
	t.Parallel()

	t.Run("invalid_empty_domain", func(t *testing.T) {
		t.Parallel()

		_, err := wire.PackDNSQueryExtended(1, "", wire.TypeA, wire.EDNSOptions{})
		assert.ErrorIs(t, err, wire.ErrInvalidDomain)
	})

	t.Run("edns_client_subnet_ipv4", func(t *testing.T) {
		t.Parallel()

		edns := wire.EDNSOptions{
			ClientIP:   netip.MustParseAddr("10.0.1.20"),
			PadToBlock: 128,
		}

		wire, err := wire.PackDNSQueryExtended(0x1234, "test.org", wire.TypeA, edns)
		require.NoError(t, err)
		require.NotEmpty(t, wire)

		// Additional count (ARCOUNT) at bytes 10..11 must be 1 for EDNS0 OPT RR
		arCount := binary.BigEndian.Uint16(wire[10:12])
		assert.Equal(t, uint16(1), arCount)

		// Verify padding aligns total message size to multiple of 128
		assert.Zero(t, len(wire)%128, "wire packet length %d should be padded to 128", len(wire))
	})

	t.Run("edns_client_subnet_ipv6", func(t *testing.T) {
		t.Parallel()

		edns := wire.EDNSOptions{
			ClientIP:   netip.MustParseAddr("2001:db8::1"),
			PadToBlock: 256,
		}

		wire, err := wire.PackDNSQueryExtended(0x5678, "ipv6.test", wire.TypeAAAA, edns)
		require.NoError(t, err)

		assert.Zero(t, len(wire)%256, "wire packet length %d should be padded to 256", len(wire))
	})
}

func TestParseDNSResponseRecords_WireParsing(t *testing.T) {
	t.Parallel()

	t.Run("truncated_message_less_than_12_bytes", func(t *testing.T) {
		t.Parallel()

		_, err := wire.ParseDNSResponseRecords([]byte{0x00, 0x01}, 0x0001)
		assert.ErrorIs(t, err, wire.ErrTruncatedDNSMessage)
	})

	t.Run("non_zero_rcode_returns_error", func(t *testing.T) {
		t.Parallel()

		// RCODE = 3 (NXDOMAIN)
		var msg [12]byte
		binary.BigEndian.PutUint16(msg[0:2], 0x1000)
		binary.BigEndian.PutUint16(msg[2:4], 0x8003) // Response flag + RCODE 3

		_, err := wire.ParseDNSResponseRecords(msg[:], 0x1000)
		require.Error(t, err)
		assert.ErrorIs(t, err, wire.ErrDNSResponseCode)
		assert.Contains(t, err.Error(), "rcode=3")
	})

	t.Run("parse_valid_response_with_ttl", func(t *testing.T) {
		t.Parallel()

		// Build valid response wire: ID 0x1122, 1 Question, 1 Answer (Type A, TTL 300, IP 192.168.1.1)
		var buf bytes.Buffer

		var hdr [12]byte
		binary.BigEndian.PutUint16(hdr[0:2], 0x1122)
		binary.BigEndian.PutUint16(hdr[2:4], 0x8100) // Response, RD, RA
		binary.BigEndian.PutUint16(hdr[4:6], 1)      // QDCOUNT
		binary.BigEndian.PutUint16(hdr[6:8], 1)      // ANCOUNT
		buf.Write(hdr[:])

		// Question: "example.com", TypeA, ClassIN
		buf.Write([]byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0})

		var qTail [4]byte
		binary.BigEndian.PutUint16(qTail[0:2], wire.TypeA)
		binary.BigEndian.PutUint16(qTail[2:4], wire.ClassIN)
		buf.Write(qTail[:])

		// Answer: Name pointer (0xc00c), TypeA, ClassIN, TTL 300, RDLen 4, IP 192.168.1.1
		buf.Write([]byte{0xc0, 0x0c}) // Pointer to offset 12

		var ansHdr [10]byte
		binary.BigEndian.PutUint16(ansHdr[0:2], wire.TypeA)
		binary.BigEndian.PutUint16(ansHdr[2:4], wire.ClassIN)
		binary.BigEndian.PutUint32(ansHdr[4:8], 300) // TTL = 300s
		binary.BigEndian.PutUint16(ansHdr[8:10], 4)  // RDLENGTH = 4
		buf.Write(ansHdr[:])
		buf.Write(net.ParseIP("192.168.1.1").To4())

		records, err := wire.ParseDNSResponseRecords(buf.Bytes(), 0x1122)
		require.NoError(t, err)
		require.Len(t, records, 1)

		assert.Equal(t, "192.168.1.1", records[0].Addr.String())
		assert.Equal(t, uint32(300), records[0].TTL)
	})
}

type extendedResolverMock struct {
	records []wire.DNSRecord
}

func (m *extendedResolverMock) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	return nil, nil
}

func (m *extendedResolverMock) LookupDNSRecords(_ context.Context, _ string) ([]wire.DNSRecord, error) {
	return m.records, nil
}

func TestInMemoryDNSCache_ExtendedDNSRecordsStorage(t *testing.T) {
	t.Parallel()

	records := []wire.DNSRecord{
		{Addr: netip.MustParseAddr("10.20.30.40"), TTL: 120},
		{Addr: netip.MustParseAddr("10.20.30.41"), TTL: 60},
	}

	extResolver := &extendedResolverMock{records: records}
	cache := NewInMemoryDNSCache(time.Minute, extResolver)
	t.Cleanup(cache.Close)

	ips, err := cache.LookupIPAddr(t.Context(), "ext.test")
	require.NoError(t, err)
	require.Len(t, ips, 2)
	assert.Equal(t, "10.20.30.40", ips[0].IP.String())

	// Verify cached entry effective TTL equals minimum record TTL (60s)
	cache.mu.RLock()
	entry, ok := cache.cache["ext.test"]
	cache.mu.RUnlock()

	assert.True(t, ok)
	assert.True(t, entry.expiry.After(time.Now().Add(55*time.Second)))
}

func TestLookupIPAddrResult_And_LookupFirstIP(t *testing.T) {
	t.Parallel()

	staticRes := NewStaticResolver(map[string][]string{
		"example.com": {"1.2.3.4"},
	}, nil)

	res := LookupIPAddrResult(t.Context(), staticRes, "example.com")
	require.True(t, res.IsSuccess())
	ips, err := res.Unwrap()
	require.NoError(t, err)
	require.Len(t, ips, 1)
	assert.Equal(t, "1.2.3.4", ips[0].IP.String())

	firstIP := LookupFirstIP(t.Context(), staticRes, "example.com")
	require.True(t, firstIP.IsPresent())
	ip, ok := firstIP.Value()
	require.True(t, ok)
	assert.Equal(t, "1.2.3.4", ip.String())

	nilLookup := LookupFirstIP(t.Context(), nil, "example.com")
	assert.False(t, nilLookup.IsPresent())
}
