// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package simd

import (
	"encoding/binary"
	"unsafe"
)

const (
	// Word64ContentType is the Little-Endian uint64 representation of "Content-".
	Word64ContentType uint64 = 0x2d746e65746e6f43
	// Word64TransferEnc is the Little-Endian uint64 representation of "Transfer".
	Word64TransferEnc uint64 = 0x726566736e617254
	// Word64UserAgent is the Little-Endian uint64 representation of "User-Age".
	Word64UserAgent uint64 = 0x6567412d72657355
	// Word64HostHeader is the Little-Endian uint64 representation of "Host:   ".
	Word64HostHeader uint64 = 0x2020203a74736f48
)

// MatchWord64 compares the first 8 bytes of buf against a target uint64 word in a single CPU instruction.
func MatchWord64(buf []byte, target uint64) bool {
	if len(buf) < 8 {
		return false
	}

	return *(*uint64)(unsafe.Pointer(&buf[0])) == target
}

// MatchWord64Str converts an 8-byte string into uint64 and compares it against buf in 1 cycle.
func MatchWord64Str(buf []byte, target string) bool {
	if len(buf) < 8 || len(target) < 8 {
		return false
	}

	targetWord := binary.LittleEndian.Uint64([]byte(target[:8]))

	return MatchWord64(buf, targetWord)
}
