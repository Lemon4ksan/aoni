// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !amd64 || purego

package gzip

import "hash/crc32"

const hasVectorCRC32 = false

func vectorCRC32Update(crc uint32, data []byte) uint32 {
	return crc32.Update(crc, crc32.IEEETable, data)
}
