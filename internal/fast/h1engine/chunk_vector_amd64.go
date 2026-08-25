// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (amd64 || arm64) && !purego

package h1engine

import (
	"unsafe"
)

const hasVectorChunk = true

func vectorParseHexUint(src []byte) (int, int, error) {
	n := len(src)
	if n == 0 {
		return 0, 0, errEmptyHexNum
	}

	var outVal uint64
	res := int(h1_parse_hex_uint(
		uint64(uintptr(unsafe.Pointer(&src[0]))),
		uint64(n),
		uint64(uintptr(unsafe.Pointer(&outVal))),
		0,
		0,
		0,
	))

	if res < 0 {
		return 0, 0, errEmptyHexNum
	}

	return int(outVal), res, nil
}

func vectorFormatHexUint(buf *[16]byte, val int) int {
	if val < 0 {
		panic("BUG: int must be positive")
	}

	return int(h1_format_hex_uint(
		uint64(uintptr(unsafe.Pointer(&buf[0]))),
		uint64(val),
		0,
		0,
		0,
		0,
	))
}
