// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// DNSResolver is an interface for DNS resolution.
type DNSResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type dnsCacheEntry struct {
	ips    []net.IPAddr
	expiry time.Time
}

// InMemoryDNSCache is an in-memory DNS cache that implements the [DNSResolver] interface.
type InMemoryDNSCache struct {
	mu       sync.RWMutex
	cache    map[string]dnsCacheEntry
	ttl      time.Duration
	resolver DNSResolver
}

// NewInMemoryDNSCache creates a new [InMemoryDNSCache] with the given TTL and resolver.
func NewInMemoryDNSCache(ttl time.Duration, r DNSResolver) *InMemoryDNSCache {
	if r == nil {
		r = &net.Resolver{}
	}

	return &InMemoryDNSCache{
		cache:    make(map[string]dnsCacheEntry),
		ttl:      ttl,
		resolver: r,
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

	ips, err := c.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[host] = dnsCacheEntry{
		ips:    ips,
		expiry: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()

	return ips, nil
}

// DoHResolver resolves DNS via HTTPS, supporting both A and AAAA records.
type DoHResolver struct {
	client   *http.Client
	endpoint string // e.g. "https://cloudflare-dns.com/dns-query"
}

// NewDoHResolver creates a [DoHResolver] with the given endpoint URL.
func NewDoHResolver(endpoint string) *DoHResolver {
	return &DoHResolver{
		client:   &http.Client{Timeout: 5 * time.Second},
		endpoint: endpoint,
	}
}

type dohResponse struct {
	Answer []dohAnswer `json:"Answer"`
}

type dohAnswer struct {
	Type int    `json:"type"` // 1 = A, 28 = AAAA
	Data string `json:"data"`
}

// LookupIPAddr queries both A and AAAA records via DoH.
func (r *DoHResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	// Query A records
	aIPs, err := r.query(ctx, host, 1)
	if err != nil {
		return nil, err
	}

	// Query AAAA records
	aaaaIPs, err := r.query(ctx, host, 28)
	if err != nil {
		return aIPs, nil //nolint:nilerr
	}

	return append(aIPs, aaaaIPs...), nil
}

func (r *DoHResolver) query(ctx context.Context, host string, qtype uint16) ([]net.IPAddr, error) {
	reqURL := fmt.Sprintf("%s?name=%s&type=%d", r.endpoint, host, qtype)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/dns-json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp dohResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	var ips []net.IPAddr
	for _, ans := range apiResp.Answer {
		if ans.Type == int(qtype) {
			ip := net.ParseIP(ans.Data)
			if ip != nil {
				ips = append(ips, net.IPAddr{IP: ip})
			}
		}
	}

	return ips, nil
}

// DoTResolver resolves DNS over TLS, querying both A and AAAA records.
// Uses github.com/miekg/dns for reliable DNS packet construction and parsing.
type DoTResolver struct {
	Server   string // e.g. "1.1.1.1:853"
	Hostname string // TLS SNI, e.g. "cloudflare-dns.com"
	Timeout  time.Duration
}

// NewDoTResolver creates a [DoTResolver] with the specified server and TLS hostname.
func NewDoTResolver(server, hostname string) *DoTResolver {
	return &DoTResolver{
		Server:   server,
		Hostname: hostname,
		Timeout:  5 * time.Second,
	}
}

// LookupIPAddr queries both A and AAAA records over TLS.
func (d *DoTResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	aIPs, err := d.lookup(ctx, host, dns.TypeA)
	if err != nil {
		return nil, err
	}

	aaaaIPs, err := d.lookup(ctx, host, dns.TypeAAAA)
	if err != nil {
		return aIPs, nil //nolint:nilerr
	}

	return append(aIPs, aaaaIPs...), nil
}

func (d *DoTResolver) lookup(ctx context.Context, host string, qtype uint16) ([]net.IPAddr, error) {
	if d.Timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, d.Timeout)
		defer cancel()
	}

	// Build DNS query using miekg/dns
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(host), qtype)
	m.RecursionDesired = true

	packed, err := m.Pack()
	if err != nil {
		return nil, fmt.Errorf("aoni dot: pack query: %w", err)
	}

	// TLS dial
	var dialer tls.Dialer

	dialer.Config = &tls.Config{ServerName: d.Hostname}

	conn, err := dialer.DialContext(ctx, "tcp", d.Server)
	if err != nil {
		return nil, fmt.Errorf("aoni dot: tls dial %s: %w", d.Server, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(d.Timeout)); err != nil {
		return nil, fmt.Errorf("aoni dot: set deadline: %w", err)
	}

	// DNS over TLS uses 2-byte length prefix (RFC 7858)
	lengthBuf := make([]byte, 2)
	lengthBuf[0] = byte(len(packed) >> 8) //nolint:gosec
	lengthBuf[1] = byte(len(packed))      //nolint:gosec

	if _, err := conn.Write(append(lengthBuf, packed...)); err != nil {
		return nil, fmt.Errorf("aoni dot: write query: %w", err)
	}

	// Read 2-byte length prefix
	if _, err := io.ReadFull(conn, lengthBuf); err != nil {
		return nil, fmt.Errorf("aoni dot: read response length: %w", err)
	}

	respLen := int(lengthBuf[0])<<8 | int(lengthBuf[1])

	respBuf := make([]byte, respLen)
	if _, err := io.ReadFull(conn, respBuf); err != nil {
		return nil, fmt.Errorf("aoni dot: read response: %w", err)
	}

	// Parse response using miekg/dns
	resp := new(dns.Msg)
	if err := resp.Unpack(respBuf); err != nil {
		return nil, fmt.Errorf("aoni dot: unpack response: %w", err)
	}

	if resp.Rcode != dns.RcodeSuccess {
		return nil, fmt.Errorf("aoni dot: DNS error rcode=%d", resp.Rcode)
	}

	var ips []net.IPAddr
	for _, answer := range resp.Answer {
		switch rr := answer.(type) {
		case *dns.A:
			ips = append(ips, net.IPAddr{IP: rr.A})
		case *dns.AAAA:
			ips = append(ips, net.IPAddr{IP: rr.AAAA})
		}
	}

	return ips, nil
}

// StdlibResolver wraps Go's standard [net.Resolver] to implement [DNSResolver].
// This is the default resolver used when no custom resolver is configured.
type StdlibResolver struct {
	Resolver *net.Resolver
}

// NewStdlibResolver creates a [StdlibResolver] with the default resolver.
func NewStdlibResolver() *StdlibResolver {
	return &StdlibResolver{Resolver: &net.Resolver{}}
}

// LookupIPAddr delegates to the underlying [net.Resolver].
func (r *StdlibResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return r.Resolver.LookupIPAddr(ctx, host)
}
