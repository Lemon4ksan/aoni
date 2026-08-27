// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns_test

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/netutil/dns"
)

type mockResolver struct {
	calls atomic.Int32
	addrs []net.IPAddr
	delay time.Duration
}

func (m *mockResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	m.calls.Add(1)

	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return m.addrs, nil
}

func TestStaleResolver_BasicAndStale(t *testing.T) {
	mock := &mockResolver{
		addrs: []net.IPAddr{{IP: net.ParseIP("192.0.2.1")}},
	}

	res := dns.NewStaleResolver(mock, func(opt *dns.StaleOptions) {
		opt.DefaultTTL = 50 * time.Millisecond
		opt.MaxExpiredTime = 1 * time.Hour
		opt.StaleDelay = 0
	})

	ctx := context.Background()

	// 1. Initial lookup (cache miss -> mock called)
	addrs, err := res.LookupIPAddr(ctx, "example.com")
	if err != nil {
		t.Fatalf("LookupIPAddr failed: %v", err)
	}

	if len(addrs) == 0 || !addrs[0].IP.Equal(net.ParseIP("192.0.2.1")) {
		t.Fatalf("unexpected addrs: %v", addrs)
	}

	if mock.calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", mock.calls.Load())
	}

	// 2. Immediate second lookup (fresh cache -> mock not called)
	addrs, err = res.LookupIPAddr(ctx, "example.com")
	if err != nil {
		t.Fatalf("LookupIPAddr second failed: %v", err)
	}

	if mock.calls.Load() != 1 {
		t.Fatalf("expected still 1 call for fresh cache, got %d", mock.calls.Load())
	}

	// 3. Wait for TTL to expire
	time.Sleep(70 * time.Millisecond)

	// 4. Third lookup (stale cache -> returns immediately, triggers background refresh)
	addrs, err = res.LookupIPAddr(ctx, "example.com")
	if err != nil {
		t.Fatalf("LookupIPAddr stale failed: %v", err)
	}

	if len(addrs) == 0 || !addrs[0].IP.Equal(net.ParseIP("192.0.2.1")) {
		t.Fatalf("unexpected stale addrs: %v", addrs)
	}

	// Wait briefly for background goroutine to execute
	time.Sleep(50 * time.Millisecond)

	if mock.calls.Load() < 2 {
		t.Fatalf("expected background refresh call >= 2, got %d", mock.calls.Load())
	}
}

func TestStaleResolver_LookupNetIP(t *testing.T) {
	mock := &mockResolver{
		addrs: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}},
	}

	res := dns.NewStaleResolver(mock)
	ctx := context.Background()

	netAddrs, err := res.LookupNetIP(ctx, "example.com")
	if err != nil {
		t.Fatalf("LookupNetIP failed: %v", err)
	}

	if len(netAddrs) == 0 || netAddrs[0].String() != "93.184.216.34" {
		t.Fatalf("unexpected netAddrs: %v", netAddrs)
	}
}
