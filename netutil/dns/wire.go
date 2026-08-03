// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
)

var (
	// ErrTruncatedDNSMessage is returned when a DNS message is shorter than the required header or payload length.
	ErrTruncatedDNSMessage = errors.New("aoni dns: truncated or malformed dns message")

	// ErrDNSResponseCode indicates that the DNS server returned a non-zero RCODE (e.g. NXDOMAIN or SERVFAIL).
	ErrDNSResponseCode = errors.New("aoni dns: server returned error response code")

	// ErrInvalidDomain is returned when an invalid or empty domain name is provided.
	ErrInvalidDomain = errors.New("aoni dns: invalid or empty domain name")
)

const (
	TypeA    uint16 = 1  // IPv4 host address (RFC 1035)
	TypeAAAA uint16 = 28 // IPv6 host address (RFC 3596)
	ClassIN  uint16 = 1  // Internet class (RFC 1035)
)

// PackDNSQuery builds a binary RFC 1035 DNS query packet for domain and query type (TypeA or TypeAAAA).
//
// Preconditions:
//   - domain must be a valid non-empty hostname (e.g. "example.com").
func PackDNSQuery(id uint16, domain string, qtype uint16) ([]byte, error) {
	if domain == "" {
		return nil, ErrInvalidDomain
	}

	buf := make([]byte, 0, 12+len(domain)+6)

	var header [12]byte
	binary.BigEndian.PutUint16(header[0:2], id)
	binary.BigEndian.PutUint16(header[2:4], 0x0100) // Standard Query, Recursion Desired (RD = 1)
	binary.BigEndian.PutUint16(header[4:6], 1)      // QDCOUNT = 1

	buf = append(buf, header[:]...)

	rest := domain
	for len(rest) > 0 {
		label := rest

		if idx := indexByte(rest, '.'); idx >= 0 {
			label = rest[:idx]
			rest = rest[idx+1:]
		} else {
			rest = ""
		}

		if len(label) == 0 {
			continue
		}

		if len(label) > 63 {
			return nil, ErrInvalidDomain
		}

		buf = append(buf, byte(len(label))) //nolint:gosec
		buf = append(buf, label...)
	}

	buf = append(buf, 0x00) // Root label terminator

	var tail [4]byte
	binary.BigEndian.PutUint16(tail[0:2], qtype)
	binary.BigEndian.PutUint16(tail[2:4], ClassIN)
	buf = append(buf, tail[:]...)

	return buf, nil
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}

	return -1
}

// ParseDNSResponse parses an RFC 1035 binary DNS response packet and extracts netip.Addr records.
func ParseDNSResponse(msg []byte, expectedID uint16) ([]netip.Addr, error) {
	if len(msg) < 12 {
		return nil, ErrTruncatedDNSMessage
	}

	id := binary.BigEndian.Uint16(msg[0:2])
	if id != 0 && expectedID != 0 && id != expectedID {
		return nil, ErrDNSResponseCode
	}

	flags := binary.BigEndian.Uint16(msg[2:4])

	rcode := flags & 0x000f
	if rcode != 0 {
		return nil, fmt.Errorf("%w: rcode=%d", ErrDNSResponseCode, rcode)
	}

	qdCount := int(binary.BigEndian.Uint16(msg[4:6]))
	anCount := int(binary.BigEndian.Uint16(msg[6:8]))

	offset := 12

	// Skip Question section
	for range qdCount {
		var err error

		offset, err = skipDomainName(msg, offset)
		if err != nil {
			return nil, err
		}

		if offset+4 > len(msg) {
			return nil, ErrTruncatedDNSMessage
		}

		offset += 4 // QTYPE (2B) + QCLASS (2B)
	}

	// Parse Answer section
	var addrs []netip.Addr

	for range anCount {
		if offset >= len(msg) {
			break
		}

		var err error

		offset, err = skipDomainName(msg, offset)
		if err != nil {
			return nil, err
		}

		if offset+10 > len(msg) {
			return nil, ErrTruncatedDNSMessage
		}

		rrType := binary.BigEndian.Uint16(msg[offset : offset+2])
		rdLength := int(binary.BigEndian.Uint16(msg[offset+8 : offset+10]))
		offset += 10

		if offset+rdLength > len(msg) {
			return nil, ErrTruncatedDNSMessage
		}

		rdata := msg[offset : offset+rdLength]
		offset += rdLength

		if rrType == TypeA && rdLength == 4 {
			var ip4 [4]byte
			copy(ip4[:], rdata)
			addrs = append(addrs, netip.AddrFrom4(ip4))
		} else if rrType == TypeAAAA && rdLength == 16 {
			var ip6 [16]byte
			copy(ip6[:], rdata)
			addrs = append(addrs, netip.AddrFrom16(ip6))
		}
	}

	return addrs, nil
}

// skipDomainName advances offset past an RFC 1035 compressed or uncompressed domain name.
func skipDomainName(msg []byte, offset int) (int, error) {
	visited := 0

	for {
		if offset >= len(msg) || visited > 128 {
			return 0, ErrTruncatedDNSMessage
		}

		length := int(msg[offset])

		// RFC 1035 Section 4.1.4: Compression pointer (top 2 bits set = 0xC0)
		if (length & 0xc0) == 0xc0 {
			if offset+2 > len(msg) {
				return 0, ErrTruncatedDNSMessage
			}

			return offset + 2, nil
		}

		if length == 0 {
			return offset + 1, nil
		}

		offset += 1 + length
		visited++
	}
}
