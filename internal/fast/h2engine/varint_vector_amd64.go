// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (amd64 || arm64) && !purego

package h2engine

import (
	"unsafe"
)

const hasVectorInt = true

func vectorAppendInt(dst []byte, bits uint8, index uint64) []byte {
	if len(dst) == 0 {
		dst = append(dst, 0)
	}

	idx := len(dst) - 1

	var stackBuf [16]byte

	stackBuf[0] = dst[idx]

	written := prefix_int_encode(
		uint64(uintptr(unsafe.Pointer(&stackBuf[0]))),
		uint64(bits),
		index,
		0,
		0,
		0,
	)

	dst[idx] = stackBuf[0]
	if written > 1 {
		dst = append(dst, stackBuf[1:written]...)
	}

	return dst
}

func vectorReadInt(n int, b []byte) ([]byte, uint64) {
	if len(b) == 0 {
		return b, 0
	}

	var (
		outVal      uint64
		outConsumed uint64
	)

	res := int64(prefix_int_decode(
		uint64(uintptr(unsafe.Pointer(&b[0]))),
		uint64(len(b)),
		uint64(n),
		uint64(uintptr(unsafe.Pointer(&outVal))),
		uint64(uintptr(unsafe.Pointer(&outConsumed))),
		0,
	))

	if res != 0 {
		// Fallback for edge cases or unexpected EOF
		return readIntFallback(n, b)
	}

	return b[outConsumed:], outVal
}
