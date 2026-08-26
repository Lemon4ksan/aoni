// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gzip

import (
	"hash/crc32"
)

// CRC32Update calculates the IEEE CRC-32 checksum update of data starting with the given initial crc.
func CRC32Update(crc uint32, data []byte) uint32 {
	return crc32.Update(crc, crc32.IEEETable, data)
}
