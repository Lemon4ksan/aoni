// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (!amd64 && !arm64) || purego

package h2engine

const hasVectorFrame = false

func vectorPackFrameHeader(dst []byte, length int, kind FrameType, flags FrameFlags, stream uint32) {
}

func vectorUnpackFrameHeader(src []byte) (length int, kind FrameType, flags FrameFlags, stream uint32) {
	return 0, 0, 0, 0
}
