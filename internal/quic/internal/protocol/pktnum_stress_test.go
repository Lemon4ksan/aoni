// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol_test

import (
	"math"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
)

func TestQUIC_DecodePacketNumber_RFC9000AppendixA(t *testing.T) {
	t.Parallel()

	// RFC 9000 Appendix A.3 official test vectors
	testCases := []struct {
		length       protocol.PacketNumberLen
		largestAcked protocol.PacketNumber
		wirePN       protocol.PacketNumber
		expected     protocol.PacketNumber
	}{
		// Sample 1: largest = 0xa82f30ea, wire = 0x9b32 (2 bytes) -> 0xa82f9b32
		{protocol.PacketNumberLen2, 0xa82f30ea, 0x9b32, 0xa82f9b32},
		// Sample 2: 1-byte encoding
		{protocol.PacketNumberLen1, 0x100, 0x01, 0x101},
		{protocol.PacketNumberLen1, 0x100, 0xff, 0xff},
		// Sample 3: 3-byte encoding
		{protocol.PacketNumberLen3, 0x12345678, 0x345679, 0x12345679},
		// Sample 4: 4-byte encoding
		{protocol.PacketNumberLen4, 0x1234567890abcdef, 0x90abcdef, 0x1234567890abcdef},
		// Boundary near 0
		{protocol.PacketNumberLen1, 0, 0, 0},
		{protocol.PacketNumberLen1, 0, 1, 1},
		// Boundary near (1<<62) - 1
		{protocol.PacketNumberLen4, protocol.PacketNumber((1 << 62) - 100), protocol.PacketNumber(((1 << 62) - 1) & 0xffffffff), protocol.PacketNumber((1 << 62) - 1)},
	}

	for _, tc := range testCases {
		decoded := protocol.DecodePacketNumber(tc.length, tc.largestAcked, tc.wirePN)
		assert.Equal(t, tc.expected, decoded)
	}

	// Exhaustive window delta test across 1, 2, 3, 4 byte lengths
	for largest := protocol.PacketNumber(0); largest < 10000; largest += 17 {
		for delta := -100; delta <= 100; delta++ {
			if int64(largest)+int64(delta) < 0 {
				continue
			}
			pn := protocol.PacketNumber(int64(largest) + int64(delta))
			len := protocol.PacketNumberLengthForHeader(pn, largest)
			var mask protocol.PacketNumber
			switch len {
			case protocol.PacketNumberLen1:
				mask = 0xff
			case protocol.PacketNumberLen2:
				mask = 0xffff
			case protocol.PacketNumberLen3:
				mask = 0xffffff
			case protocol.PacketNumberLen4:
				mask = math.MaxUint32
			}
			wirePN := pn & mask
			decoded := protocol.DecodePacketNumber(len, largest, wirePN)
			assert.Equal(t, pn, decoded)
		}
	}
}
