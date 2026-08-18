// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package simd

import (
	"encoding/binary"
	"unsafe"
)

// PackWord64 converts the first 8 bytes of string s into a Little-Endian uint64.
//
//go:inline
func PackWord64(s string) uint64 {
	if len(s) < 8 {
		var buf [8]byte
		copy(buf[:], s)

		return binary.LittleEndian.Uint64(buf[:])
	}

	return binary.LittleEndian.Uint64([]byte(s[:8]))
}

// PackWord32 converts the first 4 bytes of string s into a Little-Endian uint32.
//
//go:inline
func PackWord32(s string) uint32 {
	if len(s) < 4 {
		var buf [4]byte
		copy(buf[:], s)

		return binary.LittleEndian.Uint32(buf[:])
	}

	return binary.LittleEndian.Uint32([]byte(s[:4]))
}

// MatchWord64 compares the first 8 bytes of buf against a target uint64 word in a single CPU instruction.
//
//go:inline
func MatchWord64(buf []byte, target uint64) bool {
	if len(buf) < 8 {
		return false
	}

	return *(*uint64)(unsafe.Pointer(&buf[0])) == target
}

// MatchWord64Str converts an 8-byte string prefix into uint64 and compares it against buf in 1 CPU cycle.
//
//go:inline
func MatchWord64Str(buf []byte, target string) bool {
	if len(buf) < 8 || len(target) < 8 {
		return false
	}

	targetWord := PackWord64(target)

	return MatchWord64(buf, targetWord)
}

// MatchWord32 compares the first 4 bytes of buf against a target uint32 word in a single CPU instruction.
//
//go:inline
func MatchWord32(buf []byte, target uint32) bool {
	if len(buf) < 4 {
		return false
	}

	return *(*uint32)(unsafe.Pointer(&buf[0])) == target
}

// MatchWord32Str converts a 4-byte string prefix into uint32 and compares it against buf in 1 CPU cycle.
//
//go:inline
func MatchWord32Str(buf []byte, target string) bool {
	if len(buf) < 4 || len(target) < 4 {
		return false
	}

	targetWord := PackWord32(target)

	return MatchWord32(buf, targetWord)
}
