// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gzip_test

import (
	"crypto/rand"
	"hash/crc32"
	"testing"

	"github.com/lemon4ksan/aoni/internal/compress/gzip"
	"github.com/lemon4ksan/foundation/testkit/assert"
)

func TestCRC32_DifferentialAgainstStdlib(t *testing.T) {
	t.Parallel()

	// 1. Boundary sizes from 0 to 512 bytes
	for size := 0; size <= 512; size++ {
		data := make([]byte, size)
		_, _ = rand.Read(data)

		expected := crc32.ChecksumIEEE(data)
		actual := gzip.CRC32Update(0, data)

		assert.Equal(t, expected, actual)
	}

	// 2. Large chunks (64KB, 1MB, 4MB) with unaligned offsets
	largeSizes := []int{64 * 1024, 1024*1024 + 3, 4*1024*1024 + 7}
	for _, size := range largeSizes {
		data := make([]byte, size)
		_, _ = rand.Read(data)

		expected := crc32.ChecksumIEEE(data)
		actual := gzip.CRC32Update(0, data)

		assert.Equal(t, expected, actual)
	}

	// 3. Multi-part streaming updates: crc(A + B + C) == crc(crc(crc(0, A), B), C)
	partA := []byte("The quick brown fox ")
	partB := []byte("jumps over ")
	partC := []byte("the lazy dog.")
	full := []byte("The quick brown fox jumps over the lazy dog.")

	expected := crc32.ChecksumIEEE(full)

	var running uint32
	running = gzip.CRC32Update(running, partA)
	running = gzip.CRC32Update(running, partB)
	running = gzip.CRC32Update(running, partC)

	assert.Equal(t, expected, running)

	// 4. Fuzzing with random subslice windows
	buf := make([]byte, 4096)
	_, _ = rand.Read(buf)

	for i := 0; i < 5000; i++ {
		start := i % 2048
		end := start + (i % 2000)
		sub := buf[start:end]

		want := crc32.ChecksumIEEE(sub)
		got := gzip.CRC32Update(0, sub)

		if want != got {
			t.Fatalf("mismatch on subslice [%d:%d]: want %08x, got %08x", start, end, want, got)
		}
	}
}
