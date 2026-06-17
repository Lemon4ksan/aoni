// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
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

// DoHResolver is a DNS resolver that uses DNS-over-HTTPS (DoH).
type DoHResolver struct {
	client   *http.Client
	endpoint string // e.g., "https://cloudflare-dns.com/dns-query"
}

// NewDoHResolver creates a new [DoHResolver] with the given endpoint.
func NewDoHResolver(endpoint string) *DoHResolver {
	return &DoHResolver{
		client:   &http.Client{Timeout: 5 * time.Second},
		endpoint: endpoint,
	}
}

type dohResponse struct {
	Answer []struct {
		Type int    `json:"type"`
		Data string `json:"data"`
	} `json:"Answer"`
}

// LookupIPAddr looks up the IP addresses for the given host using the DoH resolver.
func (r *DoHResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	reqURL := fmt.Sprintf("%s?name=%s&type=A", r.endpoint, host)

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

	var ipAddrs []net.IPAddr
	for _, ans := range apiResp.Answer {
		if ans.Type == 1 { // IPv4
			ip := net.ParseIP(ans.Data)
			if ip != nil {
				ipAddrs = append(ipAddrs, net.IPAddr{IP: ip})
			}
		}
	}

	if len(ipAddrs) == 0 {
		return nil, fmt.Errorf("aoni doh: no A records for %s", host)
	}

	return ipAddrs, nil
}

// DoTResolver is a DNS resolver that uses DNS-over-TLS (DoT).
type DoTResolver struct {
	Server   string
	Hostname string
	Timeout  time.Duration
}

// NewDoTResolver creates a new DoTResolver with the specified server and hostname.
func NewDoTResolver(server, hostname string) *DoTResolver {
	return &DoTResolver{
		Server:   server,
		Hostname: hostname,
		Timeout:  5 * time.Second,
	}
}

// LookupIPAddr looks up the IP addresses for the specified host using the DoT resolver.
func (d *DoTResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	ips, err := d.lookupHost(ctx, host)
	if err != nil {
		return nil, err
	}

	var ipAddrs []net.IPAddr
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip != nil {
			ipAddrs = append(ipAddrs, net.IPAddr{IP: ip})
		}
	}

	return ipAddrs, nil
}

func (d *DoTResolver) lookupHost(ctx context.Context, host string) ([]string, error) {
	if d.Timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, d.Timeout)
		defer cancel()
	}

	var dialer tls.Dialer

	dialer.Config = &tls.Config{
		ServerName: d.Hostname,
	}

	conn, err := dialer.DialContext(ctx, "tcp", d.Server)
	if err != nil {
		return nil, fmt.Errorf("aoni dot: tls dial %s: %w", d.Server, err)
	}
	defer conn.Close()

	msg, id := buildDNSQuery(host)

	if err := conn.SetDeadline(time.Now().Add(d.Timeout)); err != nil {
		return nil, fmt.Errorf("aoni dot: set deadline: %w", err)
	}

	// DNS over TLS uses 2-byte length prefix (RFC 7858)
	lengthBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(lengthBuf, uint16(len(msg)))

	if _, err := conn.Write(append(lengthBuf, msg...)); err != nil {
		return nil, fmt.Errorf("aoni dot: write query: %w", err)
	}

	// Read 2-byte length prefix
	if _, err := io.ReadFull(conn, lengthBuf); err != nil {
		return nil, fmt.Errorf("aoni dot: read response length: %w", err)
	}

	respLen := binary.BigEndian.Uint16(lengthBuf)

	respMsg := make([]byte, respLen)
	if _, err := io.ReadFull(conn, respMsg); err != nil {
		return nil, fmt.Errorf("aoni dot: read response: %w", err)
	}

	ips, err := parseDNSResponse(respMsg, id)
	if err != nil {
		return nil, fmt.Errorf("aoni dot: parse response: %w", err)
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("aoni dot: no A records for %s", host)
	}

	return ips, nil
}

func buildDNSQuery(host string) ([]byte, uint16) {
	id := uint16(time.Now().UnixNano() & 0xFFFF)

	var buf []byte

	buf = binary.BigEndian.AppendUint16(buf, id)
	buf = binary.BigEndian.AppendUint16(buf, 0x0100) // standard query, recursion desired
	buf = binary.BigEndian.AppendUint16(buf, 1)      // QDCOUNT
	buf = binary.BigEndian.AppendUint16(buf, 0)      // ANCOUNT
	buf = binary.BigEndian.AppendUint16(buf, 0)      // NSCOUNT
	buf = binary.BigEndian.AppendUint16(buf, 0)      // ARCOUNT

	for _, label := range splitLabels(host) {
		buf = append(buf, byte(len(label)))
		buf = append(buf, label...)
	}

	buf = append(buf, 0) // root label

	buf = binary.BigEndian.AppendUint16(buf, 1) // QTYPE A
	buf = binary.BigEndian.AppendUint16(buf, 1) // QCLASS IN

	return buf, id
}

func splitLabels(host string) []string {
	var labels []string

	start := 0
	for i := 0; i <= len(host); i++ {
		if i == len(host) || host[i] == '.' {
			if i > start {
				labels = append(labels, host[start:i])
			}

			start = i + 1
		}
	}

	return labels
}

func parseDNSResponse(msg []byte, expectedID uint16) ([]string, error) {
	if len(msg) < 12 {
		return nil, errors.New("message too short")
	}

	respID := binary.BigEndian.Uint16(msg[0:2])
	if respID != expectedID {
		return nil, fmt.Errorf("ID mismatch: got %d, want %d", respID, expectedID)
	}

	flags := binary.BigEndian.Uint16(msg[2:4])
	if flags&0x8000 == 0 {
		return nil, errors.New("not a response")
	}

	rcode := flags & 0x000F
	if rcode != 0 {
		return nil, fmt.Errorf("DNS error rcode=%d", rcode)
	}

	qdcount := binary.BigEndian.Uint16(msg[4:6])
	ancount := binary.BigEndian.Uint16(msg[6:8])

	offset := 12

	// Skip questions
	for i := 0; i < int(qdcount); i++ {
		for offset < len(msg) && msg[offset] != 0 {
			if msg[offset]&0xC0 == 0xC0 {
				offset += 2
				break
			}

			offset += int(msg[offset]) + 1
		}

		if offset < len(msg) && msg[offset] == 0 {
			offset++
		}

		offset += 4 // QTYPE + QCLASS
	}

	var ips []string
	for i := 0; i < int(ancount); i++ {
		offset, err := skipName(msg, offset)
		if err != nil {
			return nil, err
		}

		if offset+10 > len(msg) {
			break
		}

		rrType := binary.BigEndian.Uint16(msg[offset : offset+2])
		offset += 2 // type
		offset += 2 // class
		offset += 4 // TTL
		rdlength := binary.BigEndian.Uint16(msg[offset : offset+2])
		offset += 2

		if offset+int(rdlength) > len(msg) {
			break
		}

		if rrType == 1 && rdlength == 4 { // A record
			ip := net.IPv4(msg[offset], msg[offset+1], msg[offset+2], msg[offset+3])
			ips = append(ips, ip.String())
		}

		offset += int(rdlength)
	}

	return ips, nil
}

func skipName(msg []byte, offset int) (int, error) {
	for offset < len(msg) {
		if msg[offset]&0xC0 == 0xC0 {
			return offset + 2, nil
		}

		if msg[offset] == 0 {
			return offset + 1, nil
		}

		offset += int(msg[offset]) + 1
	}

	return offset, errors.New("unexpected end of message in name")
}
