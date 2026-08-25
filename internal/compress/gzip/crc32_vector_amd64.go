// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64 && !purego

package gzip

import (
	"hash/crc32"
	"unsafe"
)

const hasVectorCRC32 = true

func vectorCRC32Update(crc uint32, data []byte) uint32 {
	n := len(data)
	if n == 0 {
		return crc
	}

	table := crc32.IEEETable

	res := crc32_ieee_update(
		uint64(crc),
		uint64(uintptr(unsafe.Pointer(&data[0]))),
		uint64(n),
		uint64(uintptr(unsafe.Pointer(&table[0]))),
		0,
		0,
	)

	return uint32(res)
}
