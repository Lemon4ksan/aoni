// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package dns provides secure, resilient, and anti-censorship DNS resolution strategies.
package dns

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/net/tls/cert"
	"github.com/lemon4ksan/mach/proto/dns/wire"
)

// RFC 7858 & RFC 8310 Standard DNS over TLS Port and Service Name constants.
const (
	// DoTDefaultPort is the standard IANA TCP port 853 for DNS over TLS (RFC 7858 §3.1 & §6).
	DoTDefaultPort = "853"

	// DoTServiceName is the standard IANA service name "domain-s" for DNS over TLS/DTLS (RFC 7858 §6).
	DoTServiceName = "domain-s"
)

// Standard DNS-over-(D)TLS errors defined in RFC 7858 and RFC 8310.
var (
	// ErrADNAuthenticationFailed indicates that the server's PKIX certificate does not match the Authentication Domain Name (RFC 8310 §8.1).
	ErrADNAuthenticationFailed = errors.New(
		"aoni/dns: authentication domain name verification failed (RFC 8310 §8.1)",
	)

	// ErrStrictPrivacyFailed indicates that a Strict Privacy profile connection could not be established or authenticated (RFC 8310 §6.6).
	ErrStrictPrivacyFailed = errors.New("aoni/dns: strict privacy connection failed (RFC 8310 §6.6)")
)

// PrivacyProfile defines the DNS privacy usage profile per RFC 8310 §5.
type PrivacyProfile int

const (
	// PrivacyProfileStrict requires both an encrypted and authenticated connection to a privacy-enabling DNS server (RFC 8310 §5 & §6.6).
	// Hard failure occurs if the connection cannot be established or authenticated. Falling back to unauthenticated or cleartext transport is forbidden (RFC 8310 §5.1).
	PrivacyProfileStrict PrivacyProfile = iota

	// PrivacyProfileOpportunistic attempts encryption and authentication, but permits fallback to unauthenticated or cleartext transport to maximize availability (RFC 8310 §5 & §6.5).
	PrivacyProfileOpportunistic
)

// DoTResolver resolves DNS queries over Transport Layer Security (TLS) conforming strictly to RFC 7858 and RFC 8310.
//
// Key Specifications:
// - Default port 853 with mandatory TLS handshake before any DNS message exchange (RFC 7858 §3.1).
// - All messages framed with a 2-octet big-endian length prefix passed in a single TCP write call (RFC 7858 §3.3).
// - Strict Privacy & Opportunistic Privacy usage profiles (RFC 8310 §5).
// - Authentication Domain Name (ADN) PKIX SAN validation (RFC 8310 §8.1 & RFC 6125).
// - Out-of-Band Key-Pinned Privacy Profile supporting Subject Public Key Info (SPKI) SHA-256 validation (RFC 7858 §4.2).
// - Combined authentication requiring both ADN and SPKI pins when configured (RFC 8310 §6.4).
// - EDNS0 block padding (RFC 7830 / RFC 7858 §8 & RFC 8310 §11.1) to mitigate traffic analysis.
type DoTResolver struct {
	Endpoint                 string         // e.g. "1.1.1.1:853" or "1.1.1.1" (defaults to port 853)
	Host                     string         // TLS ServerName (SNI), e.g. "cloudflare-dns.com"
	AuthenticationDomainName string         // Authentication Domain Name (ADN) for PKIX SAN verification (RFC 8310 §8.1)
	Profile                  PrivacyProfile // Usage profile: Strict (default) or Opportunistic (RFC 8310 §5)
	Timeout                  time.Duration
	TLSConfig                *tls.Config
	SPKIPins                 []string // Base64-encoded SHA-256 SPKI fingerprint pins (RFC 7858 §4.2 & RFC 8310 §6.3)
}

// NewDoTResolver creates a [DoTResolver] with the specified server endpoint and TLS hostname.
func NewDoTResolver(endpoint, host string) *DoTResolver {
	if !strings.Contains(endpoint, ":") {
		endpoint = net.JoinHostPort(endpoint, DoTDefaultPort)
	}

	return &DoTResolver{
		Endpoint:                 endpoint,
		Host:                     host,
		AuthenticationDomainName: host,
		Profile:                  PrivacyProfileStrict,
		Timeout:                  5 * time.Second,
	}
}

// WithPrivacyProfile configures the DNS privacy usage profile (RFC 8310 §5).
func (d *DoTResolver) WithPrivacyProfile(profile PrivacyProfile) *DoTResolver {
	d.Profile = profile
	return d
}

// WithAuthenticationDomainName sets the expected Authentication Domain Name (ADN) for PKIX certificate SAN matching (RFC 8310 §8.1).
func (d *DoTResolver) WithAuthenticationDomainName(adn string) *DoTResolver {
	d.AuthenticationDomainName = adn
	return d
}

// WithSPKIPins configures base64-encoded SHA-256 SPKI fingerprint pins (RFC 7858 §4.2 & RFC 7469).
func (d *DoTResolver) WithSPKIPins(pins ...string) *DoTResolver {
	d.SPKIPins = append(d.SPKIPins, pins...)
	return d
}

// LookupIPAddr queries both A and AAAA records over TLS per RFC 7858 and RFC 8310.
func (d *DoTResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	records, err := d.LookupDNSRecords(ctx, host)
	if err != nil {
		return nil, WrapDNSError(host, "DoT", d.Endpoint, err)
	}

	addrs := make([]net.IPAddr, len(records))
	for i, r := range records {
		addrs[i] = net.IPAddr{IP: net.IP(r.Addr.AsSlice())}
	}

	return addrs, nil
}

// LookupDNSRecords queries both A and AAAA records, returning IP records with authoritative TTLs.
func (d *DoTResolver) LookupDNSRecords(ctx context.Context, host string) ([]wire.DNSRecord, error) {
	if d.Timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, d.Timeout)
		defer cancel()
	}

	conn, err := d.dialTLS(ctx)
	if err != nil {
		if d.Profile == PrivacyProfileStrict {
			return nil, fmt.Errorf("%w: %w", ErrStrictPrivacyFailed, err)
		}

		return nil, err
	}
	defer conn.Close()

	var (
		v4Records  []wire.DNSRecord
		v6Records  []wire.DNSRecord
		err4, err6 error
	)

	v4Records, err4 = d.queryWireRecords(conn, host, wire.TypeA)
	v6Records, err6 = d.queryWireRecords(conn, host, wire.TypeAAAA)

	if err4 != nil && err6 != nil {
		return nil, err4
	}

	records := make([]wire.DNSRecord, 0, len(v4Records)+len(v6Records))
	records = append(records, v4Records...)
	records = append(records, v6Records...)

	return records, nil
}

// LookupWireRecord queries an arbitrary DNS record type (e.g. TypeHTTPS / TypeSVCB) over TLS.
func (d *DoTResolver) LookupWireRecord(ctx context.Context, qname string, qtype uint16) ([]byte, error) {
	if d.Timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, d.Timeout)
		defer cancel()
	}

	conn, err := d.dialTLS(ctx)
	if err != nil {
		if d.Profile == PrivacyProfileStrict {
			return nil, fmt.Errorf("%w: %w", ErrStrictPrivacyFailed, err)
		}

		return nil, err
	}
	defer conn.Close()

	return d.exchangeWire(conn, qname, qtype)
}

func (d *DoTResolver) dialTLS(ctx context.Context) (net.Conn, error) {
	var tlsCfg *tls.Config
	if d.TLSConfig != nil {
		tlsCfg = d.TLSConfig.Clone()
	} else {
		tlsCfg = &tls.Config{
			ServerName: d.Host,
			MinVersion: tls.VersionTLS12,
		}
	}

	// Prepare SPKI pins if provided
	var parsedPins [][32]byte
	if len(d.SPKIPins) > 0 {
		parsedPins = make([][32]byte, 0, len(d.SPKIPins))
		for _, pinStr := range d.SPKIPins {
			raw, err := base64.StdEncoding.DecodeString(pinStr)
			if err == nil && len(raw) == 32 {
				var pin [32]byte
				copy(pin[:], raw)
				parsedPins = append(parsedPins, pin)
			}
		}
	}

	// Authentication validation hook (RFC 8310 §6.4, §8.1 & RFC 7858 §4.2)
	adn := d.AuthenticationDomainName
	if adn != "" || len(parsedPins) > 0 {
		origVerify := tlsCfg.VerifyConnection
		tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
			// 1. ADN PKIX SAN verification (RFC 8310 §8.1 & RFC 6125)
			if adn != "" && len(cs.PeerCertificates) > 0 {
				leaf := cs.PeerCertificates[0]
				if !verifyADNSAN(leaf, adn) {
					return fmt.Errorf(
						"%w: server SANs %v do not match expected ADN %q",
						ErrADNAuthenticationFailed,
						leaf.DNSNames,
						adn,
					)
				}
			}

			// 2. SPKI Pinning verification (RFC 7858 §4.2 & RFC 8310 §6.4)
			if len(parsedPins) > 0 {
				if err := cert.ValidateChainSPKI(cs.PeerCertificates, parsedPins); err != nil {
					return fmt.Errorf("aoni/dns: dot spki pin validation failed: %w", err)
				}
			}

			if origVerify != nil {
				return origVerify(cs)
			}

			return nil
		}
	}

	dialer := tls.Dialer{Config: tlsCfg}

	conn, err := dialer.DialContext(ctx, "tcp", d.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("aoni/dns: dot tls dial %s: %w", d.Endpoint, err)
	}

	if d.Timeout > 0 {
		if err := conn.SetDeadline(time.Now().Add(d.Timeout)); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("aoni/dns: dot set deadline: %w", err)
		}
	}

	return conn, nil
}

// verifyADNSAN verifies that a leaf certificate contains the ADN in its Subject Alternative Name (SAN) extension
// as a DNS-ID per RFC 8310 §8.1 and RFC 6125 §6.
func verifyADNSAN(leaf *x509.Certificate, adn string) bool {
	if leaf == nil || len(adn) == 0 {
		return false
	}

	for _, san := range leaf.DNSNames {
		if EqualFoldASCII(san, adn) {
			return true
		}
	}

	return false
}

func (d *DoTResolver) exchangeWire(conn net.Conn, qname string, qtype uint16) ([]byte, error) {
	id := GenerateQueryID()

	// Pack DNS query with EDNS0 padding per RFC 7830 & RFC 7858 §8 & RFC 8310 §11.1
	query, err := wire.PackDNSQuery(id, qname, qtype)
	if err != nil {
		return nil, fmt.Errorf("aoni/dns: dot pack query: %w", err)
	}

	// 2-octet big-endian length prefix (RFC 7858 §3.3)
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(query)))

	// Single write call for length prefix + message (RFC 7858 §3.3 / RFC 7766 §8)
	if _, err := conn.Write(append(lenBuf[:], query...)); err != nil {
		return nil, fmt.Errorf("aoni/dns: dot write query: %w", err)
	}

	// Read 2-octet length prefix
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("aoni/dns: dot read response length: %w", err)
	}

	respLen := int(binary.BigEndian.Uint16(lenBuf[:]))
	if respLen < 12 {
		return nil, wire.ErrTruncatedDNSMessage
	}

	respBuf := make([]byte, respLen)
	if _, err := io.ReadFull(conn, respBuf); err != nil {
		return nil, fmt.Errorf("aoni/dns: dot read response payload: %w", err)
	}

	// Validate query transaction match per RFC 5452 & RFC 7858 §3.3
	respID := binary.BigEndian.Uint16(respBuf[0:2])
	if respID != id {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrSpoofedID, respID, id)
	}

	return respBuf, nil
}

func (d *DoTResolver) queryWireRecords(conn net.Conn, host string, qtype uint16) ([]wire.DNSRecord, error) {
	respBuf, err := d.exchangeWire(conn, host, qtype)
	if err != nil {
		return nil, err
	}

	respID := binary.BigEndian.Uint16(respBuf[0:2])

	return wire.ParseDNSResponseRecords(respBuf, respID)
}
