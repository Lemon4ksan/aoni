// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (amd64 || arm64) && !purego

package handshake

import "unsafe"

const hasVectorHP = true

func vectorApplyHPMask(mask []byte, firstByte *byte, hdrBytes []byte, isLongHeader bool) {
	if len(mask) < 5 || firstByte == nil {
		return
	}

	var isLong uint64
	if isLongHeader {
		isLong = 1
	}

	var hdrPtr uint64
	if len(hdrBytes) > 0 {
		hdrPtr = uint64(uintptr(unsafe.Pointer(&hdrBytes[0])))
	}

	quic_hp_mask_apply(
		uint64(uintptr(unsafe.Pointer(&mask[0]))),
		uint64(uintptr(unsafe.Pointer(firstByte))),
		hdrPtr,
		uint64(len(hdrBytes)),
		isLong,
		0,
	)
}
