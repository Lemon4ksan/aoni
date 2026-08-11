// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

const (
	typeA    uint16 = 1  // IPv4 address record
	typeAAAA uint16 = 28 // IPv6 address record
	classIN  uint16 = 1  // Internet class
)

var dotSeqID atomic.Uint32

// DoTResolver resolves DNS over TLS, querying both A and AAAA records.
// Implements native DNS wire format packing and parsing without external dependencies.
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
	aIPs, err := d.lookup(ctx, host, typeA)
	if err != nil {
		return nil, wrapDNSError(host, "DoT", d.Endpoint, err)
	}

	aaaaIPs, err := d.lookup(ctx, host, typeAAAA)
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

	id := uint16(dotSeqID.Add(1)) //nolint:gosec

	packed, err := packDNSQuery(id, host, qtype)
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

	if d.Timeout > 0 {
		if err := conn.SetDeadline(time.Now().Add(d.Timeout)); err != nil {
			return nil, fmt.Errorf("aoni: dot: set deadline: %w", err)
		}
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

	ips, err := parseDNSResponse(respBuf, id, qtype)
	if err != nil {
		return nil, fmt.Errorf("aoni: dot: parse response: %w", err)
	}

	return ips, nil
}

func packDNSQuery(id uint16, domain string, qtype uint16) ([]byte, error) {
	domain = strings.TrimSuffix(domain, ".")
	if len(domain) == 0 {
		return nil, errors.New("empty domain name")
	}

	buf := make([]byte, 0, 12+len(domain)+6)

	var header [12]byte
	binary.BigEndian.PutUint16(header[0:2], id)
	binary.BigEndian.PutUint16(header[2:4], 0x0100) // Standard Query, Recursion Desired
	binary.BigEndian.PutUint16(header[4:6], 1)      // QDCOUNT = 1

	buf = append(buf, header[:]...)

	for part := range strings.SplitSeq(domain, ".") {
		if len(part) == 0 {
			continue
		}

		if len(part) > 63 {
			return nil, fmt.Errorf("label too long %q", part)
		}

		buf = append(buf, byte(len(part))) //nolint:gosec
		buf = append(buf, part...)
	}

	buf = append(buf, 0x00) // Null byte label

	var tail [4]byte
	binary.BigEndian.PutUint16(tail[0:2], qtype)
	binary.BigEndian.PutUint16(tail[2:4], classIN)
	buf = append(buf, tail[:]...)

	return buf, nil
}

func parseDNSResponse(msg []byte, expectedID, expectedQType uint16) ([]net.IPAddr, error) {
	if len(msg) < 12 {
		return nil, errors.New("response payload too short")
	}

	id := binary.BigEndian.Uint16(msg[0:2])
	if id != expectedID {
		return nil, fmt.Errorf("transaction ID mismatch: got %d, want %d", id, expectedID)
	}

	flags := binary.BigEndian.Uint16(msg[2:4])

	rcode := flags & 0x000F
	if rcode != 0 {
		return nil, fmt.Errorf("DNS error rcode=%d", rcode)
	}

	qdCount := int(binary.BigEndian.Uint16(msg[4:6]))
	anCount := int(binary.BigEndian.Uint16(msg[6:8]))

	offset := 12

	for range qdCount {
		var err error

		offset, err = skipDNSName(msg, offset)
		if err != nil {
			return nil, err
		}

		offset += 4 // QTYPE (2 байта) + QCLASS (2 байта)
	}

	var ips []net.IPAddr

	for range anCount {
		if offset >= len(msg) {
			break
		}

		var err error

		offset, err = skipDNSName(msg, offset)
		if err != nil {
			return nil, err
		}

		if offset+10 > len(msg) {
			return nil, errors.New("truncated answer header")
		}

		rrType := binary.BigEndian.Uint16(msg[offset : offset+2])
		rdLength := int(binary.BigEndian.Uint16(msg[offset+8 : offset+10]))
		offset += 10

		if offset+rdLength > len(msg) {
			return nil, errors.New("truncated rdata")
		}

		rdata := msg[offset : offset+rdLength]
		offset += rdLength

		if rrType == expectedQType {
			if rrType == typeA && rdLength == 4 {
				ip := make(net.IP, 4)
				copy(ip, rdata)
				ips = append(ips, net.IPAddr{IP: ip})
			} else if rrType == typeAAAA && rdLength == 16 {
				ip := make(net.IP, 16)
				copy(ip, rdata)
				ips = append(ips, net.IPAddr{IP: ip})
			}
		}
	}

	return ips, nil
}

func skipDNSName(msg []byte, offset int) (int, error) {
	for {
		if offset >= len(msg) {
			return 0, errors.New("invalid name offset")
		}

		length := int(msg[offset])

		if (length & 0xC0) == 0xC0 {
			if offset+2 > len(msg) {
				return 0, errors.New("truncated pointer")
			}

			return offset + 2, nil
		}

		if length == 0 {
			return offset + 1, nil
		}

		offset += 1 + length
	}
}
