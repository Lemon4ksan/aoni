// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (!amd64 && !arm64) || purego

package quicvarint

const hasVectorAsm = false

func vectorLen(val uint64) int {
	return 0
}

func vectorParse(b []byte) (uint64, int, bool) {
	return 0, 0, false
}

func vectorAppend(dst []byte, val uint64) ([]byte, bool) {
	return dst, false
}
