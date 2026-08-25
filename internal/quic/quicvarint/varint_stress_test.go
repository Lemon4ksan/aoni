// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quicvarint_test

import (
	"crypto/rand"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"

	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

func TestQUIC_Varint_AdversarialAndBoundaries(t *testing.T) {
	t.Parallel()

	boundaryValues := []uint64{
		0,
		1,
		63,                  // 1-byte max (2^6 - 1)
		64,                  // 2-byte min
		16383,               // 2-byte max (2^14 - 1)
		16384,               // 4-byte min
		1073741823,          // 4-byte max (2^30 - 1)
		1073741824,          // 8-byte min
		4611686018427387903, // 8-byte max (2^62 - 1 / quicvarint.Max)
	}

	for _, val := range boundaryValues {
		encoded := quicvarint.Append(nil, val)
		expectedLen := quicvarint.Len(val)
		assert.Equal(t, expectedLen, len(encoded))

		decoded, consumed, err := quicvarint.Parse(encoded)
		assert.NoError(t, err)
		assert.Equal(t, expectedLen, consumed)
		assert.Equal(t, val, decoded)
	}

	// Truncated buffer checks: must return error on incomplete bytes, never panic
	for _, val := range []uint64{64, 16384, 1073741824} {
		encoded := quicvarint.Append(nil, val)
		for cut := 0; cut < len(encoded); cut++ {
			_, _, err := quicvarint.Parse(encoded[:cut])
			assert.Error(t, err)
		}
	}

	// Fuzzing Parse with random payloads
	fuzzBuf := make([]byte, 16)
	for i := 0; i < 50000; i++ {
		_, _ = rand.Read(fuzzBuf)
		_, _, _ = quicvarint.Parse(fuzzBuf[:(i%16)+1])
	}
}
