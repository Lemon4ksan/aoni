// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64 && !purego

package simd

import (
	"bytes"

	"golang.org/x/sys/cpu"
)

// indexByteAVX2 is implemented in native Go PLAN9 assembly (simd_amd64.s)
//
//go:noescape
func indexByteAVX2(b []byte, c byte) int

// IndexByteVector scans slice b for byte c using 256-bit AVX2 SIMD hardware assembly instructions.
// Falls back to IndexByteSWAR if AVX2 is not supported by CPU.
func IndexByteVector(b []byte, c byte) int {
	if len(b) >= 32 && cpu.X86.HasAVX2 {
		if idx := indexByteAVX2(b, c); idx >= 0 {
			return idx
		}

		// Handle remaining trailing < 32 bytes
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
