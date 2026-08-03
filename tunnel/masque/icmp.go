// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"encoding/binary"
	"errors"
	"net/netip"
)

var (
	// ErrInvalidIPHeader indicates that an IP packet header is truncated or malformed.
	ErrInvalidIPHeader = errors.New("aoni masque: invalid ip packet header")

	// ErrMTUTooSmall indicates that requested MTU is below the protocol minimums (68 for IPv4, 1280 for IPv6).
	ErrMTUTooSmall = errors.New("aoni masque: mtu below protocol minimum")
)

// BuildICMPPacketTooBig4 constructs an RFC 1191 / RFC 4884 ICMPv4 "Fragmentation Needed" error packet (Type 3, Code 4).
//
// Preconditions:
//   - packet must contain a valid IPv4 header (at least 20 bytes).
//   - nextHopMTU must be at least 68 octets (RFC 791/1191 minimum).
//
// Postconditions:
//   - Returns a valid ICMPv4 packet with recalculated IPv4 and ICMP checksums.
func BuildICMPPacketTooBig4(packet []byte, nextHopMTU uint16) ([]byte, error) {
	if len(packet) < 20 || (packet[0]>>4) != 4 {
		return nil, ErrInvalidIPHeader
	}

	if nextHopMTU < 68 {
		return nil, ErrMTUTooSmall
	}

	ipHdrLen := int(packet[0]&0x0f) * 4
	if len(packet) < ipHdrLen {
		return nil, ErrInvalidIPHeader
	}

	// RFC 4884 Section 3: Original datagram field MUST contain at least 128 octets
	originalLen := min(max(len(packet), 128), 500)

	// Pad to 32-bit boundary (4 bytes) per RFC 4884 Section 4
	paddedLen := (originalLen + 3) &^ 3

	// Total length: Outer IPv4 Header (20B) + ICMPv4 Header (8B) + Padded Original Datagram
	totalLen := 20 + 8 + paddedLen
	out := make([]byte, totalLen)

	// 1. Build Outer IPv4 Header
	out[0] = 0x45 // Version 4, IHL 5 (20 bytes)
	out[1] = 0x00 // TOS
	binary.BigEndian.PutUint16(out[2:4], uint16(totalLen))
	out[8] = 64 // TTL
	out[9] = 1  // Protocol 1 = ICMPv4

	// Swap IPs: Outer Src = Invoking Dest, Outer Dest = Invoking Src
	copy(out[12:16], packet[16:20])
	copy(out[16:20], packet[12:16])

	// 2. Build ICMPv4 Header (Type 3, Code 4)
	out[20] = 3 // Type 3 = Destination Unreachable
	out[21] = 4 // Code 4 = Fragmentation Needed and DF set
	out[22] = 0 // Checksum
	out[23] = 0
	out[24] = 0                                        // Unused
	out[25] = byte(paddedLen / 4)                      // RFC 4884 Section 4.1: Length in 32-bit words
	binary.BigEndian.PutUint16(out[26:28], nextHopMTU) // RFC 1191 Next-Hop MTU

	// 3. Copy Original Datagram and Pad
	copy(out[28:], packet[:min(len(packet), originalLen)])

	// 4. Calculate Checksums
	binary.BigEndian.PutUint16(out[10:12], calculateInternetChecksum(out[:20]))
	binary.BigEndian.PutUint16(out[22:24], calculateInternetChecksum(out[20:]))

	return out, nil
}

// BuildICMPPacketTooBig6 constructs an RFC 4443 ICMPv6 "Packet Too Big" error packet (Type 2, Code 0).
//
// Preconditions:
//   - packet must contain a valid IPv6 header (at least 40 bytes).
//   - nextHopMTU must be at least 1280 octets (RFC 8200 minimum IPv6 MTU).
//
// Postconditions:
//   - Returns a valid ICMPv6 packet with recalculated IPv6 Pseudo-Header checksum.
func BuildICMPPacketTooBig6(packet []byte, nextHopMTU uint32) ([]byte, error) {
	if len(packet) < 40 || (packet[0]>>4) != 6 {
		return nil, ErrInvalidIPHeader
	}

	if nextHopMTU < 1280 {
		return nil, ErrMTUTooSmall
	}

	originalLen := min(len(packet), 1200)
	totalLen := 40 + 8 + originalLen
	out := make([]byte, totalLen)

	// 1. Build Outer IPv6 Header
	out[0] = 0x60                                               // Version 6, Traffic Class 0
	binary.BigEndian.PutUint16(out[4:6], uint16(8+originalLen)) // Payload Length
	out[6] = 58                                                 // Next Header = 58 (ICMPv6)
	out[7] = 64                                                 // Hop Limit

	// Swap IPs: Outer Src = Invoking Dest, Outer Dest = Invoking Src
	copy(out[8:24], packet[24:40])
	copy(out[24:40], packet[8:24])

	// 2. Build ICMPv6 Header (Type 2, Code 0)
	out[40] = 2 // Type 2 = Packet Too Big
	out[41] = 0 // Code 0
	out[42] = 0 // Checksum
	out[43] = 0
	binary.BigEndian.PutUint32(out[44:48], nextHopMTU) // RFC 4443 32-bit MTU

	// 3. Copy Original Invoking Packet
	copy(out[48:], packet[:originalLen])

	// 4. Calculate ICMPv6 Checksum with Pseudo-Header
	srcAddr, _ := netip.AddrFromSlice(out[8:24])
	dstAddr, _ := netip.AddrFromSlice(out[24:40])

	csum := calculateICMPv6Checksum(srcAddr, dstAddr, out[40:])
	binary.BigEndian.PutUint16(out[42:44], csum)

	return out, nil
}

func calculateInternetChecksum(b []byte) uint16 {
	var sum uint32

	for i := 0; i < len(b)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i : i+2]))
	}

	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}

	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}

	return ^uint16(sum)
}

func calculateICMPv6Checksum(srcIP, dstIP netip.Addr, icmpMessage []byte) uint16 {
	var ph [40]byte

	srcBytes := srcIP.As16()
	dstBytes := dstIP.As16()

	copy(ph[0:16], srcBytes[:])
	copy(ph[16:32], dstBytes[:])
	binary.BigEndian.PutUint32(ph[32:36], uint32(len(icmpMessage)))
	ph[39] = 58 // Next Header = 58 (ICMPv6)

	var sum uint32

	// Checksum pseudo-header
	for i := 0; i < 40; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(ph[i : i+2]))
	}

	// Checksum ICMPv6 message
	for i := 0; i < len(icmpMessage)-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(icmpMessage[i : i+2]))
	}

	if len(icmpMessage)%2 == 1 {
		sum += uint32(icmpMessage[len(icmpMessage)-1]) << 8
	}

	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}

	return ^uint16(sum)
}
