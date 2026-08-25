// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"bytes"
	"net"
	"net/netip"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestEncodeDecodeVarint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		val         uint64
		expectedLen int
	}{
		{"1-byte min", 0, 1},
		{"1-byte max", 63, 1},
		{"2-byte min", 64, 2},
		{"2-byte max", 16383, 2},
		{"4-byte min", 16384, 4},
		{"4-byte max", 1073741823, 4},
		{"8-byte min", 1073741824, 8},
		{"8-byte max", 4611686018427387903, 8}, // (1<<62)-1
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buf := make([]byte, 8)
			n := EncodeVarint(tt.val, buf)
			assert.Equal(t, tt.expectedLen, n)

			decoded, decLen, err := DecodeVarint(buf[:n])
			require.NoError(t, err)
			assert.Equal(t, n, decLen)
			assert.Equal(t, tt.val, decoded)
		})
	}
}

func TestDecodeVarint_ErrorsAndBoundaries(t *testing.T) {
	t.Parallel()

	t.Run("empty buffer", func(t *testing.T) {
		t.Parallel()

		_, _, err := DecodeVarint([]byte{})
		assert.ErrorIs(t, err, ErrInvalidCapsule)
	})

	t.Run("truncated 2-byte varint", func(t *testing.T) {
		t.Parallel()

		// tag 1 (0x40) specifies 2-byte varint
		_, _, err := DecodeVarint([]byte{0x40})
		assert.ErrorIs(t, err, ErrInvalidCapsule)
	})

	t.Run("truncated 4-byte varint", func(t *testing.T) {
		t.Parallel()

		// tag 2 (0x80) specifies 4-byte varint
		_, _, err := DecodeVarint([]byte{0x80, 0x01, 0x02})
		assert.ErrorIs(t, err, ErrInvalidCapsule)
	})

	t.Run("truncated 8-byte varint", func(t *testing.T) {
		t.Parallel()

		// tag 3 (0xc0) specifies 8-byte varint
		_, _, err := DecodeVarint([]byte{0xc0, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
		assert.ErrorIs(t, err, ErrInvalidCapsule)
	})
}

func TestEncodeAddressAssignHeader(t *testing.T) {
	t.Parallel()

	buf := make([]byte, 16)
	n := EncodeAddressAssignHeader(100, buf)
	assert.Greater(t, n, 0)

	// First varint should be CapsuleAddressAssign (0x01)
	capsuleType, n1, err := DecodeVarint(buf[:n])
	require.NoError(t, err)
	assert.Equal(t, CapsuleAddressAssign, capsuleType)

	payloadLen, n2, err := DecodeVarint(buf[n1:n])
	require.NoError(t, err)
	assert.Equal(t, uint64(100), payloadLen)
	assert.Equal(t, n, n1+n2)
}

func TestDecodeAddressAssignPayload(t *testing.T) {
	t.Parallel()

	t.Run("empty payload", func(t *testing.T) {
		t.Parallel()

		entries, err := DecodeAddressAssignPayload([]byte{})
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("valid IPv4 and IPv6 entries", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		// Entry 1: IPv4 (reqID: 1, ipVer: 4, IP: 192.168.1.100, prefixLen: 32)
		var tmp [8]byte

		n := EncodeVarint(1, tmp[:])
		buf.Write(tmp[:n])
		buf.WriteByte(4) // IP version
		buf.Write(net.ParseIP("192.168.1.100").To4())
		buf.WriteByte(32) // Prefix length

		// Entry 2: IPv6 (reqID: 2, ipVer: 6, IP: 2001:db8::1, prefixLen: 128)
		n = EncodeVarint(2, tmp[:])
		buf.Write(tmp[:n])
		buf.WriteByte(6) // IP version
		buf.Write(net.ParseIP("2001:db8::1").To16())
		buf.WriteByte(128) // Prefix length

		entries, err := DecodeAddressAssignPayload(buf.Bytes())
		require.NoError(t, err)
		require.Len(t, entries, 2)

		assert.Equal(t, uint64(1), entries[0].RequestID)
		assert.Equal(t, byte(4), entries[0].IPVersion)
		assert.Equal(t, "192.168.1.100", entries[0].Addr.String())
		assert.Equal(t, byte(32), entries[0].PrefixLength)

		assert.Equal(t, uint64(2), entries[1].RequestID)
		assert.Equal(t, byte(6), entries[1].IPVersion)
		assert.Equal(t, "2001:db8::1", entries[1].Addr.String())
		assert.Equal(t, byte(128), entries[1].PrefixLength)
	})

	t.Run("corrupted varint", func(t *testing.T) {
		t.Parallel()

		// Tag 1 expects 2 bytes, but only 1 provided
		_, err := DecodeAddressAssignPayload([]byte{0x40})
		assert.ErrorIs(t, err, ErrInvalidCapsule)
	})

	t.Run("truncated before ipVer", func(t *testing.T) {
		t.Parallel()

		var tmp [8]byte

		n := EncodeVarint(10, tmp[:])
		// Write reqID, but only 1 extra byte (offset+2 > len check fails)
		payload := append(tmp[:n], byte(4))
		_, err := DecodeAddressAssignPayload(payload)
		assert.ErrorIs(t, err, ErrInvalidCapsule)
	})

	t.Run("invalid IP version", func(t *testing.T) {
		t.Parallel()

		var (
			buf bytes.Buffer
			tmp [8]byte
		)

		n := EncodeVarint(5, tmp[:])
		buf.Write(tmp[:n])
		buf.WriteByte(5) // Invalid IP version (not 4 or 6)
		buf.WriteByte(32)

		_, err := DecodeAddressAssignPayload(buf.Bytes())
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidCapsule)
		assert.Contains(t, err.Error(), "invalid IP version 5")
	})

	t.Run("truncated IP bytes", func(t *testing.T) {
		t.Parallel()

		var (
			buf bytes.Buffer
			tmp [8]byte
		)

		n := EncodeVarint(1, tmp[:])
		buf.Write(tmp[:n])
		buf.WriteByte(4)               // IPv4
		buf.Write([]byte{192, 168, 1}) // Truncated IPv4 (3 bytes instead of 4)
		buf.WriteByte(32)              // Prefix length

		_, err := DecodeAddressAssignPayload(buf.Bytes())
		assert.ErrorIs(t, err, ErrInvalidCapsule)
	})

	t.Run("truncated prefix length", func(t *testing.T) {
		t.Parallel()

		var (
			buf bytes.Buffer
			tmp [8]byte
		)

		n := EncodeVarint(1, tmp[:])
		buf.Write(tmp[:n])
		buf.WriteByte(4)                         // IPv4
		buf.Write(net.ParseIP("10.0.0.1").To4()) // 4 bytes IP, but missing prefix length

		_, err := DecodeAddressAssignPayload(buf.Bytes())
		assert.ErrorIs(t, err, ErrInvalidCapsule)
	})
}

func TestCapsuleStructs(t *testing.T) {
	t.Parallel()

	addr := AssignedAddress{
		Addr:         netip.MustParseAddr("192.168.1.1"),
		RequestID:    1,
		IPVersion:    4,
		PrefixLength: 24,
	}
	assert.Equal(t, "192.168.1.1", addr.Addr.String())
	assert.Equal(t, uint64(1), addr.RequestID)

	reqAddr := RequestedAddress{
		Addr:         netip.MustParseAddr("10.0.0.1"),
		RequestID:    2,
		IPVersion:    4,
		PrefixLength: 32,
	}
	assert.Equal(t, "10.0.0.1", reqAddr.Addr.String())

	ipRange := IPAddressRange{
		StartIP:    netip.MustParseAddr("10.0.0.1"),
		EndIP:      netip.MustParseAddr("10.0.0.255"),
		IPVersion:  4,
		IPProtocol: 17,
	}
	assert.Equal(t, "10.0.0.1", ipRange.StartIP.String())
	assert.Equal(t, byte(17), ipRange.IPProtocol)
}

func TestDecodeAddressAssignPayloadPODSlab(t *testing.T) {
	t.Parallel()

	buildPayload := func(reqID uint64, ipVer byte, ip net.IP, prefixLen byte) []byte {
		var (
			buf bytes.Buffer
			tmp [8]byte
		)

		n := EncodeVarint(reqID, tmp[:])
		buf.Write(tmp[:n])
		buf.WriteByte(ipVer)

		if ipVer == 4 {
			buf.Write(ip.To4())
		} else {
			buf.Write(ip.To16())
		}

		buf.WriteByte(prefixLen)

		return buf.Bytes()
	}

	t.Run("slab allocation - entries match expected", func(t *testing.T) {
		t.Parallel()

		slab, err := NewAssignedAddressSlab(8)
		require.NoError(t, err)

		defer slab.Release()

		var buf bytes.Buffer
		buf.Write(buildPayload(1, 4, net.ParseIP("10.0.0.1"), 24))
		buf.Write(buildPayload(2, 6, net.ParseIP("2001:db8::1"), 128))

		entries, err := DecodeAddressAssignPayloadPODSlab(slab, buf.Bytes())
		require.NoError(t, err)
		require.Len(t, entries, 2)

		assert.Equal(t, uint64(1), entries[0].RequestID)
		assert.Equal(t, byte(4), entries[0].IPVersion)
		assert.Equal(t, byte(24), entries[0].PrefixLength)

		assert.Equal(t, uint64(2), entries[1].RequestID)
		assert.Equal(t, byte(6), entries[1].IPVersion)
		assert.Equal(t, byte(128), entries[1].PrefixLength)

		// Verify slab has 2 slots in use.
		assert.Equal(t, 2, slab.Len())

		// Individual Free as each entry is processed.
		slab.Free(entries[0])
		assert.Equal(t, 1, slab.Len())

		slab.Free(entries[1])
		assert.Equal(t, 0, slab.Len())
	})

	t.Run("nil slab falls back to heap", func(t *testing.T) {
		t.Parallel()

		payload := buildPayload(42, 4, net.ParseIP("192.168.0.1"), 32)

		entries, err := DecodeAddressAssignPayloadPODSlab(nil, payload)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, uint64(42), entries[0].RequestID)
	})

	t.Run("exhausted slab falls back to heap", func(t *testing.T) {
		t.Parallel()

		// Slab with capacity 1, payload with 2 entries.
		slab, err := NewAssignedAddressSlab(1)
		require.NoError(t, err)

		defer slab.Release()

		var buf bytes.Buffer
		buf.Write(buildPayload(1, 4, net.ParseIP("10.0.0.1"), 24))
		buf.Write(buildPayload(2, 4, net.ParseIP("10.0.0.2"), 24))

		// Second entry must be heap-allocated (no panic, no nil return).
		entries, err := DecodeAddressAssignPayloadPODSlab(slab, buf.Bytes())
		require.NoError(t, err)
		require.Len(t, entries, 2)
		assert.Equal(t, uint64(1), entries[0].RequestID)
		assert.Equal(t, uint64(2), entries[1].RequestID)
	})

	t.Run("empty payload", func(t *testing.T) {
		t.Parallel()

		slab, err := NewAssignedAddressSlab(4)
		require.NoError(t, err)

		defer slab.Release()

		entries, err := DecodeAddressAssignPayloadPODSlab(slab, []byte{})
		require.NoError(t, err)
		assert.Empty(t, entries)
	})
}
