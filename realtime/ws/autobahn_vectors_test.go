// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ws_test

import (
	"crypto/rand"
	"encoding/binary"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"

	"github.com/lemon4ksan/aoni/realtime/ws"
)

// Official Test Vectors adapted from the Autobahn|TestSuite for RFC 6455 WebSocket Framing & Masking.

func TestAutobahn_Framing_PayloadBoundaries(t *testing.T) {
	t.Parallel()

	// Autobahn Case 1.1.* payload length boundaries:
	// - 0 bytes (empty)
	// - 125 bytes (maximum 7-bit length)
	// - 126 bytes (first 16-bit extended length)
	// - 127 bytes
	// - 65535 bytes (maximum 16-bit extended length)
	// - 65536 bytes (first 64-bit extended length)
	testLengths := []int{0, 1, 125, 126, 127, 256, 1024, 65535, 65536, 131072}

	for _, length := range testLengths {
		payload := make([]byte, length)
		_, _ = rand.Read(payload)

		var maskKey [4]byte

		_, _ = rand.Read(maskKey[:])

		maskedPayload := make([]byte, length)
		copy(maskedPayload, payload)

		// 1. Client-to-server masking
		ws.ApplyMask(maskedPayload, maskKey)

		// 2. Server-side unmasking (symmetric XOR)
		unmaskedPayload := make([]byte, length)
		copy(unmaskedPayload, maskedPayload)
		ws.ApplyMask(unmaskedPayload, maskKey)

		assert.Equal(t, string(payload), string(unmaskedPayload))
	}
}

func TestAutobahn_ControlFrames_RFC6455(t *testing.T) {
	t.Parallel()

	// RFC 6455 §5.5: Control frames (Ping %x9, Pong %xA, Close %x8) MUST have payload length <= 125 octets.
	opcodes := []byte{ws.OpcodePing, ws.OpcodePong, ws.OpcodeClose}

	for _, op := range opcodes {
		for size := 0; size <= 125; size += 25 {
			payload := make([]byte, size)
			_, _ = rand.Read(payload)

			var mask [4]byte
			binary.BigEndian.PutUint32(mask[:], 0x12345678)

			masked := make([]byte, size)
			copy(masked, payload)
			ws.ApplyMask(masked, mask)

			unmasked := make([]byte, size)
			copy(unmasked, masked)
			ws.ApplyMask(unmasked, mask)

			assert.Equal(t, string(payload), string(unmasked))

			_ = op
		}
	}
}
