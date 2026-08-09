// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"net/netip"

	internalMasque "github.com/lemon4ksan/aoni/internal/masque"
)

var (
	// ErrInvalidIPHeader indicates that an IP packet header is truncated or malformed.
	ErrInvalidIPHeader = internalMasque.ErrInvalidIPHeader

	// ErrMTUTooSmall indicates that requested MTU is below the protocol minimums (68 for IPv4, 1280 for IPv6).
	ErrMTUTooSmall = internalMasque.ErrMTUTooSmall
)

// BuildICMPPacketTooBig4 constructs an RFC 1191 / RFC 4884 ICMPv4 "Fragmentation Needed" error packet (Type 3, Code 4).
func BuildICMPPacketTooBig4(packet []byte, nextHopMTU uint16) ([]byte, error) {
	return internalMasque.BuildICMPPacketTooBig4(packet, nextHopMTU)
}

// BuildICMPPacketTooBig6 constructs an RFC 4443 ICMPv6 "Packet Too Big" error packet (Type 2, Code 0).
func BuildICMPPacketTooBig6(packet []byte, nextHopMTU uint32) ([]byte, error) {
	return internalMasque.BuildICMPPacketTooBig6(packet, nextHopMTU)
}

func calculateInternetChecksum(b []byte) uint16 {
	return internalMasque.CalculateInternetChecksum(b)
}

func calculateICMPv6Checksum(srcIP, dstIP netip.Addr, icmpMessage []byte) uint16 {
	return internalMasque.CalculateICMPv6Checksum(srcIP, dstIP, icmpMessage)
}
