// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"net/netip"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
)

func TestExtractSrcIP(t *testing.T) {
	t.Parallel()

	t.Run("valid IPv4 header", func(t *testing.T) {
		t.Parallel()

		// IPv4 packet with Src: 192.168.1.100 (bytes 12..15), Dst: 10.0.0.1 (bytes 16..19)
		packet := make([]byte, 20)
		packet[0] = 0x45
		copy(packet[12:16], netip.MustParseAddr("192.168.1.100").AsSlice())
		copy(packet[16:20], netip.MustParseAddr("10.0.0.1").AsSlice())

		src := ExtractSrcIP(packet)
		assert.Equal(t, netip.MustParseAddr("192.168.1.100"), src)
	})

	t.Run("valid IPv6 header", func(t *testing.T) {
		t.Parallel()

		// IPv6 packet with Src: 2001:db8::1 (bytes 8..23), Dst: 2001:db8::2 (bytes 24..39)
		packet := make([]byte, 40)
		packet[0] = 0x60
		copy(packet[8:24], netip.MustParseAddr("2001:db8::1").AsSlice())
		copy(packet[24:40], netip.MustParseAddr("2001:db8::2").AsSlice())

		src := ExtractSrcIP(packet)
		assert.Equal(t, netip.MustParseAddr("2001:db8::1"), src)
	})

	t.Run("empty or truncated packet", func(t *testing.T) {
		t.Parallel()

		assert.False(t, ExtractSrcIP([]byte{}).IsValid())
		assert.False(t, ExtractSrcIP([]byte{0x45, 0x00}).IsValid())
		assert.False(t, ExtractSrcIP([]byte{0x60, 0x00}).IsValid())
	})
}

func TestExtractDestIP(t *testing.T) {
	t.Parallel()

	t.Run("valid IPv4 header", func(t *testing.T) {
		t.Parallel()

		packet := make([]byte, 20)
		packet[0] = 0x45
		copy(packet[12:16], netip.MustParseAddr("192.168.1.100").AsSlice())
		copy(packet[16:20], netip.MustParseAddr("10.0.0.1").AsSlice())

		dst := ExtractDestIP(packet)
		assert.Equal(t, netip.MustParseAddr("10.0.0.1"), dst)
	})

	t.Run("valid IPv6 header", func(t *testing.T) {
		t.Parallel()

		packet := make([]byte, 40)
		packet[0] = 0x60
		copy(packet[8:24], netip.MustParseAddr("2001:db8::1").AsSlice())
		copy(packet[24:40], netip.MustParseAddr("2001:db8::2").AsSlice())

		dst := ExtractDestIP(packet)
		assert.Equal(t, netip.MustParseAddr("2001:db8::2"), dst)
	})

	t.Run("empty or truncated packet", func(t *testing.T) {
		t.Parallel()

		assert.False(t, ExtractDestIP([]byte{}).IsValid())
		assert.False(t, ExtractDestIP([]byte{0x45, 0x00}).IsValid())
		assert.False(t, ExtractDestIP([]byte{0x60, 0x00}).IsValid())
	})
}
