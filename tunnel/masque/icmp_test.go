// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"encoding/binary"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildICMPPacketTooBig4(t *testing.T) {
	t.Parallel()

	t.Run("valid IPv4 ICMP Packet Too Big", func(t *testing.T) {
		t.Parallel()

		// Construct sample invoking IPv4 packet (Dst: 10.0.0.2, Src: 192.168.1.100)
		packet := make([]byte, 100)
		packet[0] = 0x45 // Version 4, IHL 5
		copy(packet[12:16], netip.MustParseAddr("192.168.1.100").AsSlice())
		copy(packet[16:20], netip.MustParseAddr("10.0.0.2").AsSlice())

		icmpPkt, err := BuildICMPPacketTooBig4(packet, 1400)
		require.NoError(t, err)
		require.NotEmpty(t, icmpPkt)

		// Verify outer IPv4 header
		assert.Equal(t, byte(0x45), icmpPkt[0])
		assert.Equal(t, byte(1), icmpPkt[9])                                            // Protocol 1 (ICMPv4)
		assert.Equal(t, netip.MustParseAddr("10.0.0.2").AsSlice(), icmpPkt[12:16])      // Swapped Src
		assert.Equal(t, netip.MustParseAddr("192.168.1.100").AsSlice(), icmpPkt[16:20]) // Swapped Dst

		// Verify ICMPv4 header
		assert.Equal(t, byte(3), icmpPkt[20])                                  // Type 3 (Destination Unreachable)
		assert.Equal(t, byte(4), icmpPkt[21])                                  // Code 4 (Fragmentation Needed)
		assert.Equal(t, uint16(1400), binary.BigEndian.Uint16(icmpPkt[26:28])) // Next-hop MTU

		// Verify ICMPv4 Checksum calculation is non-zero and valid
		assert.NotZero(t, binary.BigEndian.Uint16(icmpPkt[22:24]))
	})

	t.Run("invalid IPv4 header length or version", func(t *testing.T) {
		t.Parallel()

		_, err := BuildICMPPacketTooBig4([]byte{0x45, 0x00}, 1400)
		assert.ErrorIs(t, err, ErrInvalidIPHeader)

		packet := make([]byte, 20)
		packet[0] = 0x60 // IPv6 version bit in IPv4 helper
		_, err = BuildICMPPacketTooBig4(packet, 1400)
		assert.ErrorIs(t, err, ErrInvalidIPHeader)
	})

	t.Run("MTU below IPv4 minimum 68", func(t *testing.T) {
		t.Parallel()

		packet := make([]byte, 20)
		packet[0] = 0x45
		_, err := BuildICMPPacketTooBig4(packet, 60)
		assert.ErrorIs(t, err, ErrMTUTooSmall)
	})
}

func TestBuildICMPPacketTooBig6(t *testing.T) {
	t.Parallel()

	t.Run("valid IPv6 ICMP Packet Too Big", func(t *testing.T) {
		t.Parallel()

		// Construct sample invoking IPv6 packet (Src: 2001:db8::1, Dst: 2001:db8::2)
		packet := make([]byte, 100)
		packet[0] = 0x60
		copy(packet[8:24], netip.MustParseAddr("2001:db8::1").AsSlice())
		copy(packet[24:40], netip.MustParseAddr("2001:db8::2").AsSlice())

		icmpPkt, err := BuildICMPPacketTooBig6(packet, 1350)
		require.NoError(t, err)
		require.NotEmpty(t, icmpPkt)

		// Verify outer IPv6 header
		assert.Equal(t, byte(0x60), icmpPkt[0])
		assert.Equal(t, byte(58), icmpPkt[6])                                         // Next Header = 58 (ICMPv6)
		assert.Equal(t, netip.MustParseAddr("2001:db8::2").AsSlice(), icmpPkt[8:24])  // Swapped Src
		assert.Equal(t, netip.MustParseAddr("2001:db8::1").AsSlice(), icmpPkt[24:40]) // Swapped Dst

		// Verify ICMPv6 header
		assert.Equal(t, byte(2), icmpPkt[40])                                  // Type 2 (Packet Too Big)
		assert.Equal(t, byte(0), icmpPkt[41])                                  // Code 0
		assert.Equal(t, uint32(1350), binary.BigEndian.Uint32(icmpPkt[44:48])) // 32-bit MTU

		// Verify ICMPv6 Checksum is non-zero
		assert.NotZero(t, binary.BigEndian.Uint16(icmpPkt[42:44]))
	})

	t.Run("invalid IPv6 header length or version", func(t *testing.T) {
		t.Parallel()

		_, err := BuildICMPPacketTooBig6([]byte{0x60, 0x00}, 1400)
		assert.ErrorIs(t, err, ErrInvalidIPHeader)

		packet := make([]byte, 40)
		packet[0] = 0x45 // IPv4 version bit in IPv6 helper
		_, err = BuildICMPPacketTooBig6(packet, 1400)
		assert.ErrorIs(t, err, ErrInvalidIPHeader)
	})

	t.Run("MTU below IPv6 minimum 1280", func(t *testing.T) {
		t.Parallel()

		packet := make([]byte, 40)
		packet[0] = 0x60
		_, err := BuildICMPPacketTooBig6(packet, 1200)
		assert.ErrorIs(t, err, ErrMTUTooSmall)
	})
}

func TestBuildICMPPacketTooBig_AutoDispatch(t *testing.T) {
	t.Parallel()

	t.Run("auto dispatch IPv4", func(t *testing.T) {
		t.Parallel()

		packet := make([]byte, 20)
		packet[0] = 0x45
		copy(packet[12:16], netip.MustParseAddr("192.168.1.1").AsSlice())
		copy(packet[16:20], netip.MustParseAddr("10.0.0.1").AsSlice())

		icmpPkt, err := BuildICMPPacketTooBig(packet, 1400)
		require.NoError(t, err)
		assert.Equal(t, byte(0x45), icmpPkt[0])
	})

	t.Run("auto dispatch IPv6", func(t *testing.T) {
		t.Parallel()

		packet := make([]byte, 40)
		packet[0] = 0x60
		copy(packet[8:24], netip.MustParseAddr("2001:db8::1").AsSlice())
		copy(packet[24:40], netip.MustParseAddr("2001:db8::2").AsSlice())

		icmpPkt, err := BuildICMPPacketTooBig(packet, 1400)
		require.NoError(t, err)
		assert.Equal(t, byte(0x60), icmpPkt[0])
	})

	t.Run("auto dispatch empty or invalid version", func(t *testing.T) {
		t.Parallel()

		_, err := BuildICMPPacketTooBig([]byte{}, 1400)
		assert.ErrorIs(t, err, ErrInvalidIPHeader)

		_, err = BuildICMPPacketTooBig([]byte{0x20}, 1400)
		assert.ErrorIs(t, err, ErrInvalidIPHeader)
	})
}
