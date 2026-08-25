// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (amd64 || arm64) && !purego

package brotli

import (
	"unsafe"
)

const hasVectorBitReader = true

func (br *bitReader) vectorFillBitWindow() {
	if br.bitPos < 32 {
		return
	}

	bitPos64 := uint64(br.bitPos)
	bytePos64 := uint64(br.bytePos)

	brotli_fill_bit_window(
		uint64(uintptr(unsafe.Pointer(&br.val))),
		uint64(uintptr(unsafe.Pointer(&bitPos64))),
		uint64(uintptr(unsafe.Pointer(&br.input[0]))),
		uint64(uintptr(unsafe.Pointer(&bytePos64))),
		0,
		0,
	)

	br.bitPos = uint32(bitPos64)
	br.bytePos = uint(bytePos64)
}
