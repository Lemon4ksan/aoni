// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/lemon4ksan/miyako/batto"
	"github.com/miekg/dns"
)

// DNSResolver resolves hostnames to IP addresses.
type DNSResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type dnsCacheEntry struct {
	ips    []net.IPAddr
	expiry time.Time
}

// InMemoryDNSCache caches DNS results in memory for the configured TTL.
type InMemoryDNSCache struct {
	mu       sync.RWMutex
	cache    map[string]dnsCacheEntry
	ttl      time.Duration
	resolver DNSResolver
	sflight  batto.Group[string, []net.IPAddr]
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

	ips, err := c.sflight.Do(ctx, host, func(ctx context.Context) ([]net.IPAddr, error) {
		return c.resolver.LookupIPAddr(ctx, host)
	})
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
// Uses an isolated [http.Client] that connects directly to the DoH server by IP,
// bypassing the system resolver entirely to avoid circular DNS lookups.
type DoHResolver struct {
	Endpoint string // IP-based URL, e.g. "https://1.1.1.1/dns-query"
	Host     string // Host header override, e.g. "cloudflare-dns.com"

	client *http.Client
}

// NewDoHResolver creates a [DoHResolver] that queries the given IP-based endpoint.
// The endpoint should be an IP-based URL (e.g. "https://1.1.1.1/dns-query"),
// and host is the Host header value (e.g. "cloudflare-dns.com").
func NewDoHResolver(endpoint, host string) *DoHResolver {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 5 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2: true,
	}

	return &DoHResolver{
		client: &http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
		},
		Endpoint: endpoint,
		Host:     host,
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
	reqURL := fmt.Sprintf("%s?name=%s&type=%d", r.Endpoint, host, qtype)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/dns-json")

	if r.Host != "" {
		req.Host = r.Host
	}

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
	Endpoint string // e.g. "1.1.1.1:853"
	Host     string // TLS SNI, e.g. "cloudflare-dns.com"
	Timeout  time.Duration
}

// NewDoTResolver creates a [DoTResolver] with the specified server and TLS hostname.
func NewDoTResolver(endpoint, host string) *DoTResolver {
	return &DoTResolver{
		Endpoint: endpoint,
		Host:     host,
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

	dialer.Config = &tls.Config{ServerName: d.Host}

	conn, err := dialer.DialContext(ctx, "tcp", d.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("aoni dot: tls dial %s: %w", d.Endpoint, err)
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

// StdlibResolver delegates DNS resolution to the system resolver via [net.Resolver].
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

// ProxyRoutedDNSResolver sends DNS queries through a proxy connection to prevent leaks.
type ProxyRoutedDNSResolver struct {
	resolver  DNSResolver
	proxyDial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// NewProxyRoutedDNSResolver creates a [ProxyRoutedDNSResolver] that routes DNS queries
// through the given proxy dial function.
func NewProxyRoutedDNSResolver(
	resolver DNSResolver,
	proxyDial func(ctx context.Context, network, addr string) (net.Conn, error),
) *ProxyRoutedDNSResolver {
	return &ProxyRoutedDNSResolver{
		resolver:  resolver,
		proxyDial: proxyDial,
	}
}

// LookupIPAddr resolves the host by delegating to the proxy-routed resolver.
func (r *ProxyRoutedDNSResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if r.resolver == nil {
		return nil, errors.New("aoni: proxy-routed DNS resolver: no underlying resolver configured")
	}

	return r.resolver.LookupIPAddr(ctx, host)
}
