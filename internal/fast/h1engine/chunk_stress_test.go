// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1engine_test

import (
	"crypto/rand"
	"fmt"
	"strconv"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
)

func TestH1_ParseHexUint_Adversarial(t *testing.T) {
	t.Parallel()

	// 1. Boundary & Valid Cases
	validCases := []struct {
		input    string
		expected uint64
		consumed int
	}{
		{"0", 0, 1},
		{"00", 0, 2},
		{"00000000", 0, 8},
		{"a", 10, 1},
		{"A", 10, 1},
		{"f", 15, 1},
		{"F", 15, 1},
		{"10", 16, 2},
		{"ff", 255, 2},
		{"FF", 255, 2},
		{"100", 256, 3},
		{"1a4f", 0x1a4f, 4},
		{"1A4F", 0x1a4f, 4},
		{"ffffffff", 0xffffffff, 8},
		{"7fffffffffffffff", 0x7fffffffffffffff, 16},
		{"ffffffffffffffff", 0xffffffffffffffff, 16},
		// Trailing non-hex characters (RFC 9112 §7.1 chunk extension or CRLF delimiter)
		{"1a\r\n", 0x1a, 2},
		{"ff;ext=val\r\n", 0xff, 2},
		{"0\r\n\r\n", 0, 1},
		{"deadbeef ", 0xdeadbeef, 8},
	}

	for _, tc := range validCases {
		val, n, err := h1engine.ParseHexUint([]byte(tc.input))
		assert.NoError(t, err)
		assert.Equal(t, int(tc.expected), val)
		assert.Equal(t, tc.consumed, n)
	}

	// 2. Corrupted & Malformed Cases
	invalidCases := []string{
		"",         // Empty
		"\r\n",     // No hex
		" ",        // Whitespace
		"g",        // Out of range ASCII
		"G",        // Out of range ASCII
		"z",        // Out of range ASCII
		"-1",       // Negative
		"+1",       // Plus
		":;",       // Punctuation
		"\x00",     // Null byte
		"\xff\xfe", // Binary garbage
		" 1a",      // Leading space
	}

	for _, tc := range invalidCases {
		_, _, err := h1engine.ParseHexUint([]byte(tc))
		assert.Error(t, err)
	}

	// 3. Fuzzing with random byte arrays
	buf := make([]byte, 64)
	for i := 0; i < 10000; i++ {
		_, _ = rand.Read(buf[:i%64])
		// Calling with arbitrary random bytes MUST NEVER panic or segfault
		_, _, _ = h1engine.ParseHexUint(buf[:i%64])
	}
}

func TestH1_FormatHexUint_Differential(t *testing.T) {
	t.Parallel()

	testValues := []int{
		0, 1, 2, 9, 10, 15, 16, 255, 256, 1024, 4096, 65535, 65536,
		0x12345, 0xdeadbeef, 0x7fffffff,
	}

	var buf [16]byte
	for _, val := range testValues {
		n := h1engine.FormatHexUint(&buf, val)
		expected := strconv.FormatInt(int64(val), 16)

		assert.Equal(t, len(expected), n)
		assert.Equal(t, expected, string(buf[:n]))
	}

	// 2. Exhaustive test 0..100000
	for val := 0; val < 100000; val++ {
		n := h1engine.FormatHexUint(&buf, val)
		expected := strconv.FormatInt(int64(val), 16)
		if string(buf[:n]) != expected {
			t.Fatalf("mismatch at val %d: got %s, want %s", val, string(buf[:n]), expected)
		}
	}
}

func TestH1_FormatChunkHeader_Adversarial(t *testing.T) {
	t.Parallel()

	var buf [24]byte
	for val := 0; val < 10000; val++ {
		n := h1engine.FormatChunkHeader(&buf, val)
		expected := fmt.Sprintf("%x\r\n", val)

		assert.Equal(t, len(expected), n)
		assert.Equal(t, expected, string(buf[:n]))
	}
}
