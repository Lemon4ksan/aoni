// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package wire provides utilities for encoding and decoding DNS messages.
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

var (
	// ErrTruncatedDNSMessage indicates that DNS message is shorter than the required header or payload length.
	ErrTruncatedDNSMessage = errors.New("aoni dns: truncated or malformed dns message")
	// ErrDNSResponseCode indicates that the DNS server returned a non-zero RCODE (e.g. NXDOMAIN or SERVFAIL).
	ErrDNSResponseCode = errors.New("aoni dns: server returned error response code")
	// ErrInvalidDomain indicates an invalid or empty domain name is provided.
	ErrInvalidDomain = errors.New("aoni dns: invalid or empty domain name")
	// ErrInvalidIPVersion indicates an unsupported IP version is provided for EDNS Client Subnet.
	ErrInvalidIPVersion = errors.New("aoni dns: unsupported IP version for EDNS Client Subnet")
	// ErrECHConfigNotFound indicates that the ECH config parameter was not found in the HTTPS record.
	ErrECHConfigNotFound = errors.New("aoni dns: ech config parameter not found in https record")
)

const (
	TypeA     uint16 = 1  // IPv4 host address (RFC 1035)
	TypeAAAA  uint16 = 28 // IPv6 host address (RFC 3596)
	TypeOPT   uint16 = 41 // OPT record (RFC 6891)
	TypeHTTPS uint16 = 65 // HTTPS record (RFC 9460)

	ClassIN uint16 = 1 // Internet class (RFC 1035)

	EDNS0OptionECS     uint16 = 8  // RFC 7871
	EDNS0OptionPadding uint16 = 12 // RFC 7830
)

// EDNSOptions configures Extension Mechanisms for DNS (EDNS0) features,
// including EDNS Client Subnet (ECS, RFC 7871) and Message Padding (RFC 7830).
type EDNSOptions struct {
	// ClientIP specifies the client IP address to include in the ECS option
	// for localized CDN node resolution. An invalid address disables ECS.
	ClientIP netip.Addr

	// PadToBlock specifies the block size in bytes (e.g., 128) to pad the
	// message to, preventing side-channel size analysis over encrypted transports.
	// A value <= 0 disables padding.
	PadToBlock int
}

// DNSRecord represents an IP address record along with its authoritative TTL.
type DNSRecord struct {
	Addr netip.Addr
	TTL  uint32
}

// PackDNSQuery Encodes a DNS question section into RFC 1035 wire format,
// automatically applying EDNS0 128-byte block padding for encrypted transports.
func PackDNSQuery(id uint16, domain string, qtype uint16) ([]byte, error) {
	return PackDNSQueryExtended(id, domain, qtype, EDNSOptions{PadToBlock: 128})
}

// PackDNSQueryExtended encodes a DNS question into RFC 1035 wire format,
// applying EDNS0 Extension Mechanisms (Padding and Client Subnet) if requested.
//
// Preconditions:
//   - domain must be a valid, non-empty hostname.
//
// Postconditions:
//   - Returns a binary payload with OPT RR in the Additional section if EDNS is enabled.
func PackDNSQueryExtended(id uint16, domain string, qtype uint16, edns EDNSOptions) ([]byte, error) {
	if domain == "" {
		return nil, ErrInvalidDomain
	}

	buf := make([]byte, 0, 256)

	var header [12]byte
	binary.BigEndian.PutUint16(header[0:2], id)
	binary.BigEndian.PutUint16(header[2:4], 0x0100)
	binary.BigEndian.PutUint16(header[4:6], 1)

	hasEDNS := edns.PadToBlock > 0 || edns.ClientIP.IsValid()
	if hasEDNS {
		binary.BigEndian.PutUint16(header[10:12], 1)
	}

	buf = append(buf, header[:]...)

	var err error

	buf, err = appendQName(buf, domain)
	if err != nil {
		return nil, err
	}

	var tail [4]byte
	binary.BigEndian.PutUint16(tail[0:2], qtype)
	binary.BigEndian.PutUint16(tail[2:4], ClassIN)
	buf = append(buf, tail[:]...)

	if hasEDNS {
		buf = appendEDNS0OPT(buf, edns)
	}

	return buf, nil
}

func appendQName(buf []byte, domain string) ([]byte, error) {
	rest := domain
	for len(rest) > 0 {
		label := rest
		if idx := strings.IndexByte(rest, '.'); idx >= 0 {
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

		buf = append(buf, byte(len(label)))
		buf = append(buf, label...)
	}

	return append(buf, 0x00), nil
}

func appendEDNS0OPT(buf []byte, edns EDNSOptions) []byte {
	buf = append(buf, 0x00)
	buf = appendUint16(buf, TypeOPT)
	buf = appendUint16(buf, 4096)
	buf = append(buf, 0x00, 0x00, 0x00, 0x00)

	rdLenOffset := len(buf)
	buf = appendUint16(buf, 0)
	rdataStart := len(buf)

	if edns.ClientIP.IsValid() {
		buf = appendECSOption(buf, edns.ClientIP)
	}

	if edns.PadToBlock > 0 {
		buf = appendPaddingOption(buf, edns.PadToBlock)
	}

	rdataLen := uint16(len(buf) - rdataStart)
	binary.BigEndian.PutUint16(buf[rdLenOffset:rdLenOffset+2], rdataLen)

	return buf
}

func appendECSOption(buf []byte, clientIP netip.Addr) []byte {
	var (
		family     uint16
		sourceMask byte
		ipBytes    []byte
	)

	if clientIP.Is6() {
		family = 2
		sourceMask = 56
		raw := clientIP.As16()
		ipBytes = raw[:7]
	} else {
		family = 1
		sourceMask = 24
		raw := clientIP.As4()
		ipBytes = raw[:3]
	}

	optLen := uint16(2 + 1 + 1 + len(ipBytes))
	buf = appendUint16(buf, EDNS0OptionECS)
	buf = appendUint16(buf, optLen)
	buf = appendUint16(buf, family)
	buf = append(buf, sourceMask, 0x00)

	return append(buf, ipBytes...)
}

func appendPaddingOption(buf []byte, padToBlock int) []byte {
	currentLen := len(buf) + 4
	remainder := currentLen % padToBlock

	paddingNeeded := 0
	if remainder > 0 {
		paddingNeeded = padToBlock - remainder
	}

	buf = appendUint16(buf, EDNS0OptionPadding)

	buf = appendUint16(buf, uint16(paddingNeeded))
	if paddingNeeded > 0 {
		padding := make([]byte, paddingNeeded)
		buf = append(buf, padding...)
	}

	return buf
}

func appendUint16(b []byte, v uint16) []byte {
	return append(b, byte(v>>8), byte(v))
}

// ParseDNSResponse extracts IP addresses from a binary RFC 1035 DNS response.
func ParseDNSResponse(msg []byte, expectedID uint16) ([]netip.Addr, error) {
	records, err := ParseDNSResponseRecords(msg, expectedID)
	if err != nil {
		return nil, err
	}

	addrs := make([]netip.Addr, len(records))
	for i, r := range records {
		addrs[i] = r.Addr
	}

	return addrs, nil
}

// ParseDNSResponseRecords parses an RFC 1035 binary DNS response packet
// and extracts IP address records along with their authoritative TTLs.
func ParseDNSResponseRecords(msg []byte, expectedID uint16) ([]DNSRecord, error) {
	if len(msg) < 12 {
		return nil, ErrTruncatedDNSMessage
	}

	id := binary.BigEndian.Uint16(msg[0:2])
	if id != 0 && expectedID != 0 && id != expectedID {
		return nil, ErrDNSResponseCode
	}

	flags := binary.BigEndian.Uint16(msg[2:4])
	if flags&0x000f != 0 {
		return nil, fmt.Errorf("%w: rcode=%d", ErrDNSResponseCode, flags&0x000f)
	}

	qdCount := int(binary.BigEndian.Uint16(msg[4:6]))
	anCount := int(binary.BigEndian.Uint16(msg[6:8]))

	offset := 12

	var err error

	for range qdCount {
		offset, err = SkipDomainName(msg, offset)
		if err != nil {
			return nil, err
		}

		if offset+4 > len(msg) {
			return nil, ErrTruncatedDNSMessage
		}

		offset += 4
	}

	records := make([]DNSRecord, 0, anCount)
	for range anCount {
		if offset >= len(msg) {
			break
		}

		var (
			rec      DNSRecord
			consumed int
		)

		rec, consumed, err = parseAnswerRecord(msg, offset)
		if err != nil {
			return nil, err
		}

		offset += consumed

		if rec.Addr.IsValid() {
			records = append(records, rec)
		}
	}

	return records, nil
}

func parseAnswerRecord(msg []byte, offset int) (DNSRecord, int, error) {
	nextOffset, err := SkipDomainName(msg, offset)
	if err != nil {
		return DNSRecord{}, 0, err
	}

	if nextOffset+10 > len(msg) {
		return DNSRecord{}, 0, ErrTruncatedDNSMessage
	}

	rrType := binary.BigEndian.Uint16(msg[nextOffset : nextOffset+2])
	ttl := binary.BigEndian.Uint32(msg[nextOffset+4 : nextOffset+8])
	rdLength := int(binary.BigEndian.Uint16(msg[nextOffset+8 : nextOffset+10]))
	rdataOffset := nextOffset + 10

	if rdataOffset+rdLength > len(msg) {
		return DNSRecord{}, 0, ErrTruncatedDNSMessage
	}

	totalConsumed := (rdataOffset + rdLength) - offset
	rdata := msg[rdataOffset : rdataOffset+rdLength]

	if rrType == TypeA && rdLength == 4 {
		var ip4 [4]byte
		copy(ip4[:], rdata)
		return DNSRecord{Addr: netip.AddrFrom4(ip4), TTL: ttl}, totalConsumed, nil
	}

	if rrType == TypeAAAA && rdLength == 16 {
		var ip6 [16]byte
		copy(ip6[:], rdata)
		return DNSRecord{Addr: netip.AddrFrom16(ip6), TTL: ttl}, totalConsumed, nil
	}

	return DNSRecord{}, totalConsumed, nil
}

// SkipDomainName skips over a domain name in a DNS message, following DNS compression pointers if necessary.
func SkipDomainName(msg []byte, offset int) (int, error) {
	visited := 0

	for {
		if offset >= len(msg) || visited > 128 {
			return 0, ErrTruncatedDNSMessage
		}

		length := int(msg[offset])
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

// ExtractECHFromHTTPSResponse extracts raw ECHConfigList bytes from an RFC 9460 HTTPS (Type 65) DNS response packet.
func ExtractECHFromHTTPSResponse(msg []byte, expectedID uint16) ([]byte, error) {
	if len(msg) < 12 {
		return nil, ErrTruncatedDNSMessage
	}

	id := binary.BigEndian.Uint16(msg[0:2])
	if id != 0 && expectedID != 0 && id != expectedID {
		return nil, ErrDNSResponseCode
	}

	qdCount := int(binary.BigEndian.Uint16(msg[4:6]))
	anCount := int(binary.BigEndian.Uint16(msg[6:8]))

	offset := 12

	var err error

	for range qdCount {
		offset, err = SkipDomainName(msg, offset)
		if err != nil {
			return nil, err
		}

		if offset+4 > len(msg) {
			return nil, ErrTruncatedDNSMessage
		}

		offset += 4
	}

	for range anCount {
		if offset >= len(msg) {
			break
		}

		var (
			ech      []byte
			consumed int
		)

		ech, consumed, err = parseHTTPSRecord(msg, offset)
		if err == nil && len(ech) > 0 {
			return ech, nil
		}

		if consumed == 0 {
			break
		}

		offset += consumed
	}

	return nil, ErrECHConfigNotFound
}

func parseHTTPSRecord(msg []byte, offset int) ([]byte, int, error) {
	nextOffset, err := SkipDomainName(msg, offset)
	if err != nil {
		return nil, 0, err
	}

	if nextOffset+10 > len(msg) {
		return nil, 0, ErrTruncatedDNSMessage
	}

	rrType := binary.BigEndian.Uint16(msg[nextOffset : nextOffset+2])
	rdLength := int(binary.BigEndian.Uint16(msg[nextOffset+8 : nextOffset+10]))
	rdataOffset := nextOffset + 10

	if rdataOffset+rdLength > len(msg) {
		return nil, 0, ErrTruncatedDNSMessage
	}

	totalConsumed := (rdataOffset + rdLength) - offset
	if rrType != TypeHTTPS || rdLength < 4 {
		return nil, totalConsumed, nil
	}

	rdata := msg[rdataOffset : rdataOffset+rdLength]
	svcOffset := 2 // Skip SvcPriority

	svcOffset, err = SkipDomainName(rdata, svcOffset)
	if err != nil {
		return nil, totalConsumed, nil //nolint:nilerr
	}

	for svcOffset+4 <= len(rdata) {
		paramKey := binary.BigEndian.Uint16(rdata[svcOffset : svcOffset+2])
		paramLen := int(binary.BigEndian.Uint16(rdata[svcOffset+2 : svcOffset+4]))
		svcOffset += 4

		if svcOffset+paramLen > len(rdata) {
			break
		}

		// SvcParamKey 5 = "ech"
		if paramKey == 5 {
			echBytes := make([]byte, paramLen)
			copy(echBytes, rdata[svcOffset:svcOffset+paramLen])
			return echBytes, totalConsumed, nil
		}

		svcOffset += paramLen
	}

	return nil, totalConsumed, nil
}
