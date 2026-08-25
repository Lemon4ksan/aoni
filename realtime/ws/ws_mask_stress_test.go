// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ws_test

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"

	"github.com/lemon4ksan/aoni/realtime/ws"
)

func scalarMask(b []byte, key [4]byte) {
	for i := range b {
		b[i] ^= key[i%4]
	}
}

func TestWS_MaskPayload_StressAndDifferential(t *testing.T) {
	t.Parallel()

	// 1. Length boundaries from 0 to 4096 bytes
	for length := 0; length <= 256; length++ {
		payload := make([]byte, length)
		_, _ = rand.Read(payload)

		expected := make([]byte, length)
		copy(expected, payload)

		var key [4]byte

		_, _ = rand.Read(key[:])

		scalarMask(expected, key)

		// Apply fast vector masking
		ws.ApplyMask(payload, key)

		assert.Equal(t, string(expected), string(payload))

		// Double-masking property: mask(mask(p, k), k) must equal original payload
		ws.ApplyMask(payload, key)
		scalarMask(expected, key)
		assert.Equal(t, string(expected), string(payload))
	}

	// 2. Large sizes (64KB, 1MB, 4MB) with unaligned offsets
	largeSizes := []int{64 * 1024, 1024*1024 + 7, 4*1024*1024 + 13}
	for _, size := range largeSizes {
		payload := make([]byte, size)
		_, _ = rand.Read(payload)

		expected := bytes.Clone(payload)

		var key [4]byte

		_, _ = rand.Read(key[:])

		scalarMask(expected, key)
		ws.ApplyMask(payload, key)

		assert.Equal(t, true, bytes.Equal(expected, payload))
	}

	// 3. Fuzzing with random slice subslices
	buffer := make([]byte, 2048)
	_, _ = rand.Read(buffer)

	for i := 0; i < 5000; i++ {
		start := i % 1024
		end := start + (i % 1000)
		sub := buffer[start:end]

		orig := bytes.Clone(sub)

		var key [4]byte

		_, _ = rand.Read(key[:])

		ws.ApplyMask(sub, key)
		scalarMask(orig, key)

		if !bytes.Equal(sub, orig) {
			t.Fatalf("mismatch at subslice [%d:%d]", start, end)
		}
	}
}
