// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/miekg/dns"
)

// DoTResolver resolves DNS over TLS, querying both A and AAAA records.
// Uses github.com/miekg/dns for reliable DNS packet construction and parsing.
// If TLSConfig is nil, a default config is used with TLS 1.2 minimum version.
type DoTResolver struct {
	Endpoint  string // e.g. "1.1.1.1:853"
	Host      string // TLS SNI, e.g. "cloudflare-dns.com"
	Timeout   time.Duration
	TLSConfig *tls.Config
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
		return nil, wrapDNSError(host, "DoT", d.Endpoint, err)
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
		return nil, fmt.Errorf("aoni: dot: pack query: %w", err)
	}

	// TLS dial
	var dialer tls.Dialer
	if d.TLSConfig != nil {
		dialer.Config = d.TLSConfig
	} else {
		dialer.Config = &tls.Config{
			ServerName: d.Host,
			MinVersion: tls.VersionTLS12,
		}
	}

	conn, err := dialer.DialContext(ctx, "tcp", d.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("aoni: dot: tls dial %s: %w", d.Endpoint, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(d.Timeout)); err != nil {
		return nil, fmt.Errorf("aoni: dot: set deadline: %w", err)
	}

	// DNS over TLS uses 2-byte length prefix (RFC 7858)
	lengthBuf := make([]byte, 2)
	lengthBuf[0] = byte(len(packed) >> 8) //nolint:gosec
	lengthBuf[1] = byte(len(packed))      //nolint:gosec

	if _, err := conn.Write(append(lengthBuf, packed...)); err != nil {
		return nil, fmt.Errorf("aoni: dot: write query: %w", err)
	}

	// Read 2-byte length prefix
	if _, err := io.ReadFull(conn, lengthBuf); err != nil {
		return nil, fmt.Errorf("aoni: dot: read response length: %w", err)
	}

	respLen := int(lengthBuf[0])<<8 | int(lengthBuf[1])

	respBuf := make([]byte, respLen)
	if _, err := io.ReadFull(conn, respBuf); err != nil {
		return nil, fmt.Errorf("aoni: dot: read response: %w", err)
	}

	// Parse response using miekg/dns
	resp := new(dns.Msg)
	if err := resp.Unpack(respBuf); err != nil {
		return nil, fmt.Errorf("aoni: dot: unpack response: %w", err)
	}

	if resp.Rcode != dns.RcodeSuccess {
		return nil, fmt.Errorf("aoni: dot: DNS error rcode=%d", resp.Rcode)
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
