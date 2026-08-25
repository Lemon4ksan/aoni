// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build (!amd64 && !arm64) || purego

package h2engine

const hasVectorHuffman = false

func vectorHuffmanEncode(dst, src []byte) []byte {
	return dst
}

func vectorHuffmanEncodeLength(src []byte) int {
	return 0
}

func vectorHuffmanDecode(dst, src []byte) []byte {
	return dst
}
