// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64 && !purego

package simd

import (
	"bytes"
	"unsafe"

	"golang.org/x/sys/cpu"
)

var hasAVX2 = cpu.X86.HasAVX2

//go:noescape
func indexByteAVX2(b []byte, c byte) int

//go:noescape
func indexTwoBytesAVX2(b []byte, c1, c2 byte) int

//go:noescape
func applyFastMaskAVX2(b []byte, mask uint32)

// IndexByteVector scans slice b for byte c using 256-bit AVX2 SIMD hardware assembly instructions.
func IndexByteVector(b []byte, c byte) int {
	if len(b) >= 32 && hasAVX2 {
		if idx := indexByteAVX2(b, c); idx >= 0 {
			return idx
		}

		rem := len(b) &^ 31
		if rem < len(b) {
			if idx := bytes.IndexByte(b[rem:], c); idx >= 0 {
				return rem + idx
			}
		}

		return -1
	}

	return IndexByteSWAR(b, c)
}

// IndexTwoBytesVector searches for the first occurrence of c1 or c2 using 256-bit AVX2 SIMD hardware assembly.
func IndexTwoBytesVector(b []byte, c1, c2 byte) int {
	if len(b) >= 32 && hasAVX2 {
		if idx := indexTwoBytesAVX2(b, c1, c2); idx >= 0 {
			return idx
		}

		rem := len(b) &^ 31
		if rem < len(b) {
			if idx := IndexByteTwoSWAR(b[rem:], c1, c2); idx >= 0 {
				return rem + idx
			}
		}

		return -1
	}

	return IndexByteTwoSWAR(b, c1, c2)
}

// ApplyFastMaskVector masks slice b using a 4-byte mask via 256-bit AVX2 VPXOR vector instructions.
func ApplyFastMaskVector(b []byte, mask uint32) {
	if len(b) >= 32 && hasAVX2 {
		applyFastMaskAVX2(b, mask)

		rem := len(b) &^ 31
		if rem < len(b) {
			maskSWAR(b[rem:], mask)
		}

		return
	}

	maskSWAR(b, mask)
}

func maskSWAR(b []byte, mask uint32) {
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
