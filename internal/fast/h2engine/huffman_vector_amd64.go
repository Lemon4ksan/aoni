// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64 && !purego

package h2engine

import (
	"unsafe"
)

const hasVectorHuffman = true

func vectorHuffmanEncode(dst, src []byte) []byte {
	nSrc := len(src)
	if nSrc == 0 {
		return dst
	}

	symTablePtr := uint64(uintptr(unsafe.Pointer(&huffmanSymTable[0])))

	needed := int(hpack_huffman_encode_count(
		uint64(uintptr(unsafe.Pointer(&src[0]))),
		uint64(nSrc),
		symTablePtr,
		0,
		0,
		0,
	))

	start := len(dst)

	total := start + needed
	if cap(dst) < total {
		newDst := make([]byte, total)
		copy(newDst, dst)
		dst = newDst
	} else {
		dst = dst[:total]
	}

	written := hpack_huffman_encode(
		uint64(uintptr(unsafe.Pointer(&dst[start]))),
		uint64(uintptr(unsafe.Pointer(&src[0]))),
		uint64(nSrc),
		symTablePtr,
		0,
		0,
	)

	return dst[:start+int(written)]
}

func vectorHuffmanEncodeLength(src []byte) int {
	if len(src) == 0 {
		return 0
	}

	return int(hpack_huffman_encode_count(
		uint64(uintptr(unsafe.Pointer(&src[0]))),
		uint64(len(src)),
		uint64(uintptr(unsafe.Pointer(&huffmanSymTable[0]))),
		0,
		0,
		0,
	))
}

func vectorHuffmanDecode(dst, src []byte) []byte {
	nSrc := len(src)
	if nSrc == 0 {
		return dst
	}

	// Maximum possible decoded expansion is ~2x src length (shortest Huffman code is 5 bits for 8-bit symbol)
	maxCap := len(dst) + nSrc*2 + 32

	var (
		stackBuf [512]byte
		workBuf  []byte
	)

	if maxCap <= len(stackBuf) {
		workBuf = stackBuf[:]
	} else {
		workBuf = make([]byte, maxCap)
	}

	res := int64(hpack_huffman_decode(
		uint64(uintptr(unsafe.Pointer(&workBuf[0]))),
		uint64(uintptr(unsafe.Pointer(&src[0]))),
		uint64(nSrc),
		uint64(uintptr(unsafe.Pointer(&huffmanDecodeTable[0]))),
		0,
		0,
	))

	if res < 0 {
		// Fallback to table decoder
		return huffmanDecodeFallback(dst, src)
	}

	return append(dst, workBuf[:res]...)
}
