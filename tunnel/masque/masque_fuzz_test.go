// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/tunnel/masque"
)

// FuzzMASQUEVarint tests RFC 9000 QUIC / MASQUE variable-length integer decoding and roundtripping.
func FuzzMASQUEVarint(f *testing.F) {
	f.Add([]byte{0x25})                                           // 1-byte: 37
	f.Add([]byte{0x7B, 0xBD})                                     // 2-byte: 15293
	f.Add([]byte{0x9D, 0x7F, 0x3E, 0x7D})                         // 4-byte: 494878333
	f.Add([]byte{0xC2, 0x19, 0x7C, 0x5E, 0xFF, 0x14, 0xE8, 0x8C}) // 8-byte: 151288809941952652
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		val, n, err := masque.DecodeVarint(data)
		if err == nil {
			if n <= 0 || n > len(data) {
				t.Fatalf("invalid bytes read %d for data len %d", n, len(data))
			}

			// Encode back into stack slice and verify roundtrip
			var tmp [8]byte

			encN := masque.EncodeVarint(val, tmp[:])
			if encN > 0 {
				val2, n2, err2 := masque.DecodeVarint(tmp[:encN])
				if err2 != nil || val2 != val || n2 != encN {
					t.Fatalf("varint roundtrip mismatch: got %d (len %d), expected %d (len %d)", val2, n2, val, encN)
				}
			}
		}
	})
}

// FuzzIPPacketExtract tests IPv4/IPv6 packet header inspection and checksum resilience.
func FuzzIPPacketExtract(f *testing.F) {
	f.Add(
		[]byte{
			0x45,
			0x00,
			0x00,
			0x3c,
			0x00,
			0x00,
			0x40,
			0x00,
			0x40,
			0x06,
			0x00,
			0x00,
			0x7f,
			0x00,
			0x00,
			0x01,
			0x01,
			0x01,
			0x01,
			0x01,
		},
	)
	f.Add([]byte{0x60, 0x00, 0x00, 0x00, 0x00, 0x00, 0x06, 0x40})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, packet []byte) {
		_ = masque.ExtractDestIP(packet)
		_ = masque.ExtractSrcIP(packet)
	})
}
