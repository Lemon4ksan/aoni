// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (!amd64 && !arm64) || purego

package simd

import "unsafe"

// IndexByteVector falls back to SWAR on non-amd64 architectures.
func IndexByteVector(b []byte, c byte) int {
	return IndexByteSWAR(b, c)
}

// IndexTwoBytesVector falls back to SWAR on non-amd64 architectures.
func IndexTwoBytesVector(b []byte, c1, c2 byte) int {
	return IndexByteTwoSWAR(b, c1, c2)
}

// ApplyFastMaskVector falls back to SWAR 64-bit XOR masking on non-amd64 architectures.
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

// PrefetchL1 is a no-op fallback for non-amd64 architectures.
func PrefetchL1(_ unsafe.Pointer) {}

// StreamCopy256 falls back to standard copy on non-amd64 architectures.
func StreamCopy256(dst, src []byte) int {
	return copy(dst, src)
}
