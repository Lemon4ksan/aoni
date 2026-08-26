// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (!amd64 && !arm64) || purego

package h1engine

const hasVectorChunk = false

func vectorParseHexUint(src []byte) (int, int, error) {
	return parseHexUintFallback(src)
}

func vectorFormatHexUint(buf *[16]byte, val int) int {
	return formatHexUintFallback(buf, val)
}

func vectorFormatChunkHeader(buf *[24]byte, val int) int {
	n := formatHexUintFallback((*[16]byte)(buf[:16]), val)
	buf[n] = '\r'
	buf[n+1] = '\n'
	return n + 2
}
