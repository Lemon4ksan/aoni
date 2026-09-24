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

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"

	"github.com/lemon4ksan/aoni/dns"
)

type mockResolver struct {
	fn func(ctx context.Context, host string) ([]net.IPAddr, error)
}

func (m *mockResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if m.fn != nil {
		return m.fn(ctx, host)
	}

	return nil, errors.New("mock not implemented")
}

func TestStdlibResolver(t *testing.T) {
	t.Parallel()

	r := dns.NewStdlibResolver()
	assert.NotNil(t, r)
	assert.NotNil(t, r.Resolver)

	// ResolverFunc
	customCalled := false
	rf := dns.ResolverFunc(func(ctx context.Context, host string) ([]net.IPAddr, error) {
		customCalled = true
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	})

	ips, err := rf.LookupIPAddr(context.Background(), "localhost")
	require.NoError(t, err)
	assert.True(t, customCalled)
	assert.Len(t, ips, 1)
}

func TestStaticResolver(t *testing.T) {
	t.Parallel()

	mapping := map[string][]string{
		"api.local": {"10.0.0.1", "10.0.0.2"},
	}
	delegate := &mockResolver{
		fn: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			if host == "fallback.local" {
				return []net.IPAddr{{IP: net.ParseIP("192.168.1.1")}}, nil
			}

			return nil, dns.ErrNXDomain
		},
	}

	sr := dns.NewStaticResolver(mapping, delegate)

	// Static hit
	ips, err := sr.LookupIPAddr(context.Background(), "api.local.")
	require.NoError(t, err)
	assert.Len(t, ips, 2)
	assert.Equal(t, "10.0.0.1", ips[0].IP.String())

	// Delegate fallback
	ips, err = sr.LookupIPAddr(context.Background(), "fallback.local")
	require.NoError(t, err)
	assert.Len(t, ips, 1)
	assert.Equal(t, "192.168.1.1", ips[0].IP.String())

	// Non-matching
	_, err = sr.LookupIPAddr(context.Background(), "unknown.local")
	assert.ErrorIs(t, err, dns.ErrNXDomain)
}

func TestFallbackResolver(t *testing.T) {
	t.Parallel()

	r1 := &mockResolver{fn: func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return nil, errors.New("r1 failed")
	}}
	r2 := &mockResolver{fn: func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
	}}

	fr := dns.NewFallbackResolver(r1, r2)
	ips, err := fr.LookupIPAddr(context.Background(), "example.com")
	require.NoError(t, err)
	assert.Len(t, ips, 1)
	assert.Equal(t, "1.1.1.1", ips[0].IP.String())

	// Empty resolvers
	emptyFR := dns.NewFallbackResolver()
	_, errEmpty := emptyFR.LookupIPAddr(context.Background(), "example.com")
	assert.ErrorIs(t, errEmpty, dns.ErrNoResolversConfigured)
}

func TestFastRaceResolver(t *testing.T) {
	t.Parallel()

	slowR := &mockResolver{fn: func(ctx context.Context, host string) ([]net.IPAddr, error) {
		select {
		case <-time.After(200 * time.Millisecond):
			return []net.IPAddr{{IP: net.ParseIP("2.2.2.2")}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	fastR := &mockResolver{fn: func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
	}}

	raceR := dns.NewFastRaceResolver(slowR, fastR)
	ips, err := raceR.LookupIPAddr(context.Background(), "example.com")
	require.NoError(t, err)
	assert.Len(t, ips, 1)
	assert.Equal(t, "1.1.1.1", ips[0].IP.String())
}

func TestProxyRoutedDNSResolver(t *testing.T) {
	t.Parallel()

	mock := &mockResolver{fn: func(ctx context.Context, host string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("9.9.9.9")}}, nil
	}}

	pr := dns.NewProxyRoutedDNSResolver(mock, nil)
	ips, err := pr.LookupIPAddr(context.Background(), "example.com")
	require.NoError(t, err)
	assert.Equal(t, "9.9.9.9", ips[0].IP.String())

	prNil := dns.NewProxyRoutedDNSResolver(nil, nil)
	_, errNil := prNil.LookupIPAddr(context.Background(), "example.com")
	assert.ErrorIs(t, errNil, dns.ErrNoResolversConfigured)
}

func TestLookupResultAndOptional(t *testing.T) {
	t.Parallel()

	r := &mockResolver{fn: func(ctx context.Context, host string) ([]net.IPAddr, error) {
		if host == "found.com" {
			return []net.IPAddr{{IP: net.ParseIP("1.2.3.4")}}, nil
		}

		return nil, errors.New("not found")
	}}

	// LookupIPAddrResult
	res := dns.LookupIPAddrResult(context.Background(), r, "found.com")
	assert.True(t, res.IsSuccess())
	ips, _ := res.Unwrap()
	assert.Len(t, ips, 1)

	resErr := dns.LookupIPAddrResult(context.Background(), r, "missing.com")
	assert.False(t, resErr.IsSuccess())

	// LookupFirstIP
	opt := dns.LookupFirstIP(context.Background(), r, "found.com")
	assert.True(t, opt.IsPresent())
	ip, ok := opt.Value()
	assert.True(t, ok)
	assert.Equal(t, "1.2.3.4", ip.String())

	optNone := dns.LookupFirstIP(context.Background(), r, "missing.com")
	assert.False(t, optNone.IsPresent())
}
