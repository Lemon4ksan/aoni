// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64 && !purego

package ws

import (
	"unsafe"
)

const hasVectorWS = true

func vectorApplyFastMask(payload []byte, mask [4]byte) {
	n := len(payload)
	if n == 0 {
		return
	}

	maskKey := *(*uint32)(unsafe.Pointer(&mask[0]))

	ws_mask_xor(
		uint64(uintptr(unsafe.Pointer(&payload[0]))),
		uint64(n),
		uint64(maskKey),
		0,
		0,
		0,
	)
}

func vectorBuildFrameHeader(dst []byte, opcode byte, length int, compress, isClient bool) int {
	var compVal, clientVal uint64
	if compress {
		compVal = 1
	}

	if isClient {
		clientVal = 1
	}

	written := ws_build_frame_header(
		uint64(uintptr(unsafe.Pointer(&dst[0]))),
		uint64(opcode),
		uint64(length),
		compVal,
		clientVal,
		0,
	)

	return int(written)
}
