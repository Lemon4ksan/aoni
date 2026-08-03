// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import "net/netip"

// ExtractDestIP extracts the destination IP address from raw IPv4 or IPv6 header bytes with 0 B/op allocations.
func ExtractDestIP(packet []byte) netip.Addr {
	if len(packet) == 0 {
		return netip.Addr{}
	}

	version := packet[0] >> 4

	// IPv4 Header: Destination IP resides at bytes 16-19
	if version == 4 && len(packet) >= 20 {
		var ip4 [4]byte
		copy(ip4[:], packet[16:20])

		return netip.AddrFrom4(ip4)
	}

	// IPv6 Header: Destination IP resides at bytes 24-39
	if version == 6 && len(packet) >= 40 {
		var ip6 [16]byte
		copy(ip6[:], packet[24:40])

		return netip.AddrFrom16(ip6)
	}

	return netip.Addr{}
}

// ExtractSrcIP extracts the source IP address from raw IPv4 or IPv6 header bytes with 0 B/op allocations.
func ExtractSrcIP(packet []byte) netip.Addr {
	if len(packet) == 0 {
		return netip.Addr{}
	}

	version := packet[0] >> 4

	// IPv4 Header: Source IP resides at bytes 12-15
	if version == 4 && len(packet) >= 20 {
		var ip4 [4]byte
		copy(ip4[:], packet[12:16])

		return netip.AddrFrom4(ip4)
	}

	// IPv6 Header: Source IP resides at bytes 8-23
	if version == 6 && len(packet) >= 40 {
		var ip6 [16]byte
		copy(ip6[:], packet[8:24])

		return netip.AddrFrom16(ip6)
	}

	return netip.Addr{}
}
