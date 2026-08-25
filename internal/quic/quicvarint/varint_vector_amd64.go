// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64 && !purego

package quicvarint

import "unsafe"

const hasVectorAsm = true

func vectorLen(val uint64) int {
	return int(quic_varint_len(val, 0, 0, 0, 0, 0))
}

func vectorParse(b []byte) (uint64, int, bool) {
	if len(b) == 0 {
		return 0, 0, false
	}

	var val, consumed uint64

	ret := int64(quic_varint_parse(
		uint64(uintptr(unsafe.Pointer(&b[0]))),
		uint64(len(b)),
		uint64(uintptr(unsafe.Pointer(&val))),
		uint64(uintptr(unsafe.Pointer(&consumed))),
		0,
		0,
	))
	if ret != 0 {
		return 0, 0, false
	}

	return val, int(consumed), true
}

func vectorAppend(dst []byte, val uint64) ([]byte, bool) {
	if val > maxVarInt8 {
		return dst, false
	}

	needed := int(quic_varint_len(val, 0, 0, 0, 0, 0))
	origLen := len(dst)

	if cap(dst)-origLen < needed {
		newDst := make([]byte, origLen+needed, max(cap(dst)*2, origLen+needed))
		copy(newDst, dst)
		dst = newDst
	} else {
		dst = dst[:origLen+needed]
	}

	n := quic_varint_append(
		uint64(uintptr(unsafe.Pointer(&dst[origLen]))),
		val,
		0,
		0,
		0,
		0,
	)
	if int64(n) < 0 {
		return dst[:origLen], false
	}

	return dst, true
}
