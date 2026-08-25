// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !amd64 || purego

package h1engine

const hasVectorChunk = false

func vectorParseHexUint(src []byte) (int, int, error) {
	return 0, 0, errEmptyHexNum
}

func vectorFormatHexUint(buf *[16]byte, val int) int {
	return 0
}
