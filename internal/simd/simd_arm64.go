// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build arm64 && !purego

package simd

import (
	"bytes"
	"unsafe"
)

//go:noescape
func indexByteNEON(b []byte, c byte) int

//go:noescape
func indexTwoBytesNEON(b []byte, c1, c2 byte) int

// IndexByteVector uses 128-bit NEON hardware vector scanning on ARM64 when available.
func IndexByteVector(b []byte, c byte) int {
	if len(b) >= 16 {
		if idx := indexByteNEON(b, c); idx >= 0 {
			return idx
		}
	}

	return bytes.IndexByte(b, c)
}

// IndexTwoBytesVector uses 128-bit NEON hardware vector scanning on ARM64 when available.
func IndexTwoBytesVector(b []byte, c1, c2 byte) int {
	if len(b) >= 16 {
		if idx := indexTwoBytesNEON(b, c1, c2); idx >= 0 {
			return idx
		}
	}

	return IndexByteTwoSWAR(b, c1, c2)
}

// ApplyFastMaskVector falls back to SWAR 64-bit XOR masking on ARM64.
func ApplyFastMaskVector(b []byte, mask uint32) {
	if len(b) == 0 {
		return
	}

	mask64 := uint64(mask) | (uint64(mask) << 32)
	i := 0

	for i+8 <= len(b) {
		*(*uint64)(unsafe.Pointer(&b[i])) ^= mask64
		i += 8
	}

	maskBytes := [4]byte{
		byte(mask),
		byte(mask >> 8),
		byte(mask >> 16),
		byte(mask >> 24),
	}

	for ; i < len(b); i++ {
		b[i] ^= maskBytes[i&3]
	}
}

func extractBitsHW(val, mask uint64) uint64 {
	return extractBitsSWAR(val, mask)
}

func depositBitsHW(val, mask uint64) uint64 {
	return depositBitsSWAR(val, mask)
}

// PrefetchL1 is a no-op fallback for ARM64 architectures.
func PrefetchL1(_ unsafe.Pointer) {}

// StreamCopy256 falls back to standard copy on ARM64 architectures.
func StreamCopy256(dst, src []byte) int {
	return copy(dst, src)
}
