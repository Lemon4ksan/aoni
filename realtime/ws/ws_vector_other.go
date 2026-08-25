// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (!amd64 && !arm64) || purego

package ws

const hasVectorWS = false

func vectorApplyFastMask(payload []byte, mask [4]byte) {
}

func vectorBuildFrameHeader(dst []byte, opcode byte, length int, compress bool, isClient bool) int {
	return 0
}
