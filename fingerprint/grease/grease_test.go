// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grease_test

import (
	"slices"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/fingerprint/grease"
)

func TestRFC8701_16BitValues(t *testing.T) {
	t.Parallel()

	expectedValues := []uint16{
		0x0A0A, 0x1A1A, 0x2A2A, 0x3A3A,
		0x4A4A, 0x5A5A, 0x6A6A, 0x7A7A,
		0x8A8A, 0x9A9A, 0xAAAA, 0xBABA,
		0xCACA, 0xDADA, 0xEAEA, 0xFAFA,
	}

	assert.Equal(t, len(expectedValues), len(grease.All16BitValues))

	for _, val := range expectedValues {
		assert.True(t, grease.Is(val), "0x%04X should be GREASE", val)
		assert.True(t, grease.IsUint16(val), "0x%04X should be GREASE", val)
		assert.True(t, grease.IsBytes([]byte{byte(val >> 8), byte(val)}))
	}

	nonGreaseValues := []uint16{
		0x0000, 0x0001, 0x002B, 0x0033, 0x1301, 0x1302, 0x1303,
		0xC02F, 0xC030, 0x0303, 0x0304, 0x0A0B, 0x1A2A, 0xFAFB,
	}

	for _, val := range nonGreaseValues {
		assert.False(t, grease.Is(val), "0x%04X should not be GREASE", val)
		assert.False(t, grease.IsUint16(val), "0x%04X should not be GREASE", val)
	}

	assert.False(t, grease.IsBytes([]byte{0x0A}))
	assert.False(t, grease.IsBytes([]byte{0x0A, 0x0A, 0x0A}))
}

func TestRFC8701_PskModes(t *testing.T) {
	t.Parallel()

	expectedModes := []uint8{
		0x0B, 0x2A, 0x49, 0x68,
		0x87, 0xA6, 0xC5, 0xE4,
	}

	assert.Equal(t, len(expectedModes), len(grease.AllPskModes))

	for _, mode := range expectedModes {
		assert.True(t, grease.IsUint8(mode), "0x%02X should be GREASE PSK mode", mode)
	}

	nonGreaseModes := []uint8{0x00, 0x01, 0x02, 0x0A, 0x0C, 0xFF}
	for _, mode := range nonGreaseModes {
		assert.False(t, grease.IsUint8(mode), "0x%02X should not be GREASE PSK mode", mode)
	}
}

func TestRFC8701_ALPN(t *testing.T) {
	t.Parallel()

	assert.True(t, grease.IsALPN(string([]byte{0x0A, 0x0A})))
	assert.True(t, grease.IsALPN(string([]byte{0xFA, 0xFA})))
	assert.False(t, grease.IsALPN("h2"))
	assert.False(t, grease.IsALPN("http/1.1"))
	assert.False(t, grease.IsALPN(string([]byte{0x0A, 0x0B})))
	assert.False(t, grease.IsALPN(""))
	assert.False(t, grease.IsALPN("h"))

	alpns := []string{
		string([]byte{0x2A, 0x2A}),
		"h2",
		string([]byte{0x4A, 0x4A}),
		"http/1.1",
	}

	filtered := grease.FilterALPN(alpns)
	assert.Equal(t, []string{"h2", "http/1.1"}, filtered)
}

func TestRFC8701_Filter_And_FilterInPlace(t *testing.T) {
	t.Parallel()

	input := []uint16{
		0x0A0A, // GREASE
		0x1301, // TLS_AES_128_GCM_SHA256
		0x1302, // TLS_AES_256_GCM_SHA384
		0x2A2A, // GREASE
		0xC02F, // ECDHE_RSA_WITH_AES_128_GCM_SHA256
		0xFAFA, // GREASE
	}

	filtered := grease.Filter(input)
	assert.Equal(t, []uint16{0x1301, 0x1302, 0xC02F}, filtered)
	// Ensure input wasn't modified
	assert.Len(t, input, 6)

	inPlace := slices.Clone(input)
	resInPlace := grease.FilterInPlace(inPlace)
	assert.Equal(t, []uint16{0x1301, 0x1302, 0xC02F}, resInPlace)
}

func TestRFC8701_RandomGeneration(t *testing.T) {
	t.Parallel()

	for range 50 {
		u16 := grease.RandomUint16()
		assert.True(t, grease.Is(u16))

		u8 := grease.RandomUint8()
		assert.True(t, grease.IsUint8(u8))

		alpn := grease.RandomALPN()
		assert.True(t, grease.IsALPN(alpn))

		alpnBytes := grease.RandomALPNBytes()
		assert.True(t, grease.IsBytes(alpnBytes))
	}
}

func TestRFC8701_ValidateServerNegotiation(t *testing.T) {
	t.Parallel()

	t.Run("valid_negotiation", func(t *testing.T) {
		t.Parallel()

		err := grease.ValidateServerNegotiation(0x0303, 0x1301, []uint16{0x002B, 0x0033})
		assert.NoError(t, err)
	})

	t.Run("illegal_version_grease", func(t *testing.T) {
		t.Parallel()

		err := grease.ValidateServerNegotiation(0x0A0A, 0x1301, []uint16{0x002B})
		require.ErrorIs(t, err, grease.ErrNegotiatedGREASE)
	})

	t.Run("illegal_cipher_grease", func(t *testing.T) {
		t.Parallel()

		err := grease.ValidateServerNegotiation(0x0303, 0x2A2A, []uint16{0x002B})
		require.ErrorIs(t, err, grease.ErrNegotiatedGREASE)
	})

	t.Run("illegal_extension_grease", func(t *testing.T) {
		t.Parallel()

		err := grease.ValidateServerNegotiation(0x0303, 0x1301, []uint16{0x002B, 0x3A3A})
		require.ErrorIs(t, err, grease.ErrNegotiatedGREASE)
	})
}
