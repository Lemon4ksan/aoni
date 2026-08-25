// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (amd64 || arm64) && !purego

package h2engine

import (
	"unsafe"
)

const hasVectorFrame = true

func vectorPackFrameHeader(dst []byte, length int, kind FrameType, flags FrameFlags, stream uint32) {
	_ = dst[8]
	h2_frame_header_pack(
		uint64(uintptr(unsafe.Pointer(&dst[0]))),
		uint64(length),
		uint64(kind),
		uint64(flags),
		uint64(stream),
		0,
	)
}

func vectorUnpackFrameHeader(src []byte) (length int, kind FrameType, flags FrameFlags, stream uint32) {
	_ = src[8]

	var outLen, outKind, outFlags, outStream uint64

	h2_frame_header_unpack(
		uint64(uintptr(unsafe.Pointer(&src[0]))),
		uint64(uintptr(unsafe.Pointer(&outLen))),
		uint64(uintptr(unsafe.Pointer(&outKind))),
		uint64(uintptr(unsafe.Pointer(&outFlags))),
		uint64(uintptr(unsafe.Pointer(&outStream))),
		0,
	)

	return int(outLen), FrameType(outKind), FrameFlags(outFlags), uint32(outStream)
}
