// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (amd64 || arm64) && !purego

package qpack

import (
	"io"
	"unsafe"
)

const hasVectorInt = true

func vectorAppendInt(dst []byte, prefixLen uint8, val uint64) []byte {
	if prefixLen > 8 || prefixLen == 0 {
		panic("invalid prefix length")
	}

	// Ensure destination has enough room for prefix integer (up to 10 bytes)
	idx := len(dst) - 1

	var stackBuf [16]byte

	stackBuf[0] = dst[idx]

	written := prefix_int_encode(
		uint64(uintptr(unsafe.Pointer(&stackBuf[0]))),
		uint64(prefixLen),
		val,
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

func vectorReadInt(prefixLen uint8, data []byte) (uint64, int, error) {
	if len(data) == 0 {
		return 0, 0, io.ErrUnexpectedEOF
	}

	if prefixLen > 8 || prefixLen == 0 {
		return 0, 0, errInvalidInteger
	}

	var (
		outVal      uint64
		outConsumed uint64
	)

	res := int64(prefix_int_decode(
		uint64(uintptr(unsafe.Pointer(&data[0]))),
		uint64(len(data)),
		uint64(prefixLen),
		uint64(uintptr(unsafe.Pointer(&outVal))),
		uint64(uintptr(unsafe.Pointer(&outConsumed))),
		0,
	))

	switch res {
	case 0:
		return outVal, int(outConsumed), nil
	case -1:
		return 0, 0, io.ErrUnexpectedEOF
	case -2:
		return 0, 0, errIntegerOverflow
	default:
		return 0, 0, errInvalidInteger
	}
}
