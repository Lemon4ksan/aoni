// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (amd64 || arm64) && !purego

package fse

import (
	"encoding/binary"
	"unsafe"
)

const hasVectorFSE = true

func vectorDecodeQuad(s1, s2 *decoder, dt []decSymbol, tmp []byte, off uint8) uint8 {
	res := fse_decode_quad(
		uint64(uintptr(unsafe.Pointer(&s1.state))),
		uint64(uintptr(unsafe.Pointer(&s2.state))),
		s1.br.value,
		uint64(uintptr(unsafe.Pointer(&s1.br.bitsRead))),
		uint64(uintptr(unsafe.Pointer(&dt[0]))),
		0,
	)

	binary.LittleEndian.PutUint32(tmp[off:], uint32(res))
	return off + 4
}
