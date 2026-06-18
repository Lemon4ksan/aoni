// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"testing"
	"time"

	"github.com/lemon4ksan/aoni"
)

func TestNewDoTResolver(t *testing.T) {
	resolver := aoni.NewDoTResolver("1.1.1.1:853", "cloudflare-dns.com")
	if resolver == nil {
		t.Fatal("NewDoTResolver returned nil")
	}

	if resolver.Endpoint != "1.1.1.1:853" {
		t.Errorf("Server = %q, want %q", resolver.Endpoint, "1.1.1.1:853")
	}

	if resolver.Host != "cloudflare-dns.com" {
		t.Errorf("Hostname = %q, want %q", resolver.Host, "cloudflare-dns.com")
	}

	if resolver.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want %v", resolver.Timeout, 5*time.Second)
	}
}

func TestDoTResolverLookupHost(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DNS-over-TLS test in short mode")
	}

	resolver := aoni.NewDoTResolver("1.1.1.1:853", "cloudflare-dns.com")
	resolver.Timeout = 5 * time.Second

	ipAddrs, err := resolver.LookupIPAddr(t.Context(), "example.com")
	if err != nil {
		t.Skipf("DNS-over-TLS lookup failed (network may be unavailable): %v", err)
	}

	if len(ipAddrs) == 0 {
		t.Error("expected at least one IP address")
	}

	for _, ipAddr := range ipAddrs {
		if ipAddr.IP == nil {
			t.Error("nil IP in response")
		}
	}
}

func TestNewInMemoryDNSCache(t *testing.T) {
	cache := aoni.NewInMemoryDNSCache(time.Minute, nil)
	if cache == nil {
		t.Fatal("NewInMemoryDNSCache returned nil")
	}
}

func TestNewDoHResolver(t *testing.T) {
	resolver := aoni.NewDoHResolver("https://8.8.8.8/dns-query", "dns.google")
	if resolver == nil {
		t.Fatal("NewDoHResolver returned nil")
	}
}
