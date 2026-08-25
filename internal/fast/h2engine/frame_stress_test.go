// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine_test

import (
	"crypto/rand"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"

	"github.com/lemon4ksan/aoni/internal/fast/h2engine"
)

func TestH2_FrameHeader_Adversarial(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		length   int
		fType    h2engine.FrameType
		flags    h2engine.FrameFlags
		streamID uint32
	}{
		{0, 0, 0, 0},
		{1, 1, 1, 1},
		{16384, 0x1, 0x5, 3},
		{16777215, 0x9, 0xff, 0x7fffffff},
		{500, 0x0, 0x1, 0x80000001},
		{100, 0x4, 0x0, 0xffffffff},
	}

	fr := h2engine.AcquireFrameHeader()
	defer h2engine.ReleaseFrameHeader(fr)

	for _, tc := range testCases {
		fr.Reset()
		fr.SetStream(tc.streamID)
		fr.SetFlags(tc.flags)

		var buf [9]byte
		h2engine.PackFrameHeader(buf[:], tc.length, tc.fType, tc.flags, tc.streamID)
		length, kind, flags, stream := h2engine.UnpackFrameHeader(buf[:])

		expectedStreamID := tc.streamID & 0x7fffffff
		expectedLength := tc.length & 0x00ffffff

		assert.Equal(t, expectedLength, length)
		assert.Equal(t, tc.fType, kind)
		assert.Equal(t, tc.flags, flags)
		assert.Equal(t, expectedStreamID, stream)
	}

	// Fuzzing decode
	var buf [9]byte
	for i := 0; i < 50000; i++ {
		_, _ = rand.Read(buf[:])
		length, _, _, stream := h2engine.UnpackFrameHeader(buf[:])
		assert.Equal(t, true, length <= 0x00ffffff)
		assert.Equal(t, true, stream <= 0x7fffffff)
	}
}

func TestH2_Varint_Adversarial(t *testing.T) {
	t.Parallel()

	prefixes := []uint8{1, 2, 3, 4, 5, 6, 7, 8}
	values := []uint64{
		0, 1, 2, 10, 30, 31, 32, 63, 64, 127, 128, 255, 256,
		1000, 65535, 100000, 0xffffffff, 0x7fffffffffffffff,
	}

	for _, n := range prefixes {
		for _, val := range values {
			encoded := h2engine.AppendInt(nil, n, val)
			rem, decoded := h2engine.ReadInt(int(n), encoded)
			assert.Equal(t, 0, len(rem))
			assert.Equal(t, val, decoded)
		}
	}

	// Truncated buffer test (must not panic)
	for _, n := range prefixes {
		var b [1]byte

		b[0] = (1 << n) - 1
		rem, decoded := h2engine.ReadInt(int(n), b[:])
		assert.Equal(t, 0, len(rem))
		assert.Equal(t, uint64(b[0]), decoded)
	}

	// Fuzzing decode with random byte streams
	fuzzBuf := make([]byte, 32)
	for i := 0; i < 20000; i++ {
		_, _ = rand.Read(fuzzBuf)
		prefix := int((i % 8) + 1)
		_, _ = h2engine.ReadInt(prefix, fuzzBuf[:(i%32)+1])
	}
}

func TestH2_Huffman_Adversarial(t *testing.T) {
	t.Parallel()

	testStrings := []string{
		"",
		"a",
		":status",
		"200",
		"content-type",
		"application/json; charset=utf-8",
		"https://example.com/api/v1/users/42?filter=active&sort=desc",
		"custom-header-with-long-value-1234567890-abcdefghijklmnopqrstuvwxyz",
	}

	for _, s := range testStrings {
		encoded := h2engine.HuffmanEncode(nil, []byte(s))
		decoded := h2engine.HuffmanDecode(nil, encoded)
		assert.Equal(t, s, string(decoded))
	}

	// Corrupted Huffman byte streams must decode cleanly or partially, never crash
	garbage := make([]byte, 64)
	for i := 0; i < 10000; i++ {
		_, _ = rand.Read(garbage)
		_ = h2engine.HuffmanDecode(nil, garbage[:(i%64)+1])
	}
}
