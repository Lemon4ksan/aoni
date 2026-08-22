// Copyright 2013 Google Inc. All Rights Reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli

import "encoding/binary"

var kBitMask = [33]uint32{
	0x00000000, 0x00000001, 0x00000003, 0x00000007,
	0x0000000F, 0x0000001F, 0x0000003F, 0x0000007F,
	0x000000FF, 0x000001FF, 0x000003FF, 0x000007FF,
	0x00000FFF, 0x00001FFF, 0x00003FFF, 0x00007FFF,
	0x0000FFFF, 0x0001FFFF, 0x0003FFFF, 0x0007FFFF,
	0x000FFFFF, 0x001FFFFF, 0x003FFFFF, 0x007FFFFF,
	0x00FFFFFF, 0x01FFFFFF, 0x03FFFFFF, 0x07FFFFFF,
	0x0FFFFFFF, 0x1FFFFFFF, 0x3FFFFFFF, 0x7FFFFFFF,
	0xFFFFFFFF,
}

const shortFillBitWindowRead = 4

func bitMask(n uint32) uint32 {
	return kBitMask[n]
}

// bitReader manages little-endian bit-level stream reading with a 64-bit sliding accumulator.
type bitReader struct {
	val      uint64
	bitPos   uint32
	input    []byte
	inputLen uint
	bytePos  uint
}

type bitReaderState struct {
	val      uint64
	bitPos   uint32
	input    []byte
	inputLen uint
	bytePos  uint
}

func bitReaderSaveState(from *bitReader, to *bitReaderState) {
	to.val = from.val
	to.bitPos = from.bitPos
	to.input = from.input
	to.inputLen = from.inputLen
	to.bytePos = from.bytePos
}

func bitReaderRestoreState(to *bitReader, from *bitReaderState) {
	to.val = from.val
	to.bitPos = from.bitPos
	to.input = from.input
	to.inputLen = from.inputLen
	to.bytePos = from.bytePos
}

func getAvailableBits(br *bitReader) uint32 {
	return 64 - br.bitPos
}

func getRemainingBytes(br *bitReader) uint {
	return uint(uint32(br.inputLen-br.bytePos) + (getAvailableBits(br) >> 3))
}

func checkInputAmount(br *bitReader, num uint) bool {
	return br.inputLen-br.bytePos >= num
}

func fillBitWindow(br *bitReader, nBits uint32) {
	if br.bitPos >= 32 {
		br.val >>= 32
		br.bitPos ^= 32
		br.val |= (uint64(binary.LittleEndian.Uint32(br.input[br.bytePos:]))) << 32
		br.bytePos += 4
	}
}

func fillBitWindow16(br *bitReader) {
	fillBitWindow(br, 17)
}

func pullByte(br *bitReader) bool {
	if br.bytePos == br.inputLen {
		return false
	}

	br.val >>= 8
	br.val |= (uint64(br.input[br.bytePos])) << 56
	br.bitPos -= 8
	br.bytePos++

	return true
}

func getBitsUnmasked(br *bitReader) uint64 {
	return br.val >> br.bitPos
}

func get16BitsUnmasked(br *bitReader) uint32 {
	fillBitWindow(br, 16)
	return uint32(getBitsUnmasked(br))
}

func getBits(br *bitReader, nBits uint32) uint32 {
	fillBitWindow(br, nBits)
	return uint32(getBitsUnmasked(br)) & bitMask(nBits)
}

func safeGetBits(br *bitReader, nBits uint32, val *uint32) bool {
	for getAvailableBits(br) < nBits {
		if !pullByte(br) {
			return false
		}
	}

	*val = uint32(getBitsUnmasked(br)) & bitMask(nBits)

	return true
}

func dropBits(br *bitReader, nBits uint32) {
	br.bitPos += nBits
}

func bitReaderUnload(br *bitReader) {
	unusedBytes := getAvailableBits(br) >> 3
	unusedBits := unusedBytes << 3

	br.bytePos -= uint(unusedBytes)
	if unusedBits == 64 {
		br.val = 0
	} else {
		br.val <<= unusedBits
	}

	br.bitPos += unusedBits
}

func takeBits(br *bitReader, nBits uint32, val *uint32) {
	*val = uint32(getBitsUnmasked(br)) & bitMask(nBits)
	dropBits(br, nBits)
}

func readBits(br *bitReader, nBits uint32) uint32 {
	var val uint32

	fillBitWindow(br, nBits)
	takeBits(br, nBits, &val)

	return val
}

func safeReadBits(br *bitReader, nBits uint32, val *uint32) bool {
	for getAvailableBits(br) < nBits {
		if !pullByte(br) {
			return false
		}
	}

	takeBits(br, nBits, val)

	return true
}

func bitReaderJumpToByteBoundary(br *bitReader) bool {
	padBitsCount := getAvailableBits(br) & 0x7

	var padBits uint32
	if padBitsCount != 0 {
		takeBits(br, padBitsCount, &padBits)
	}

	return padBits == 0
}

func copyBytes(dest []byte, br *bitReader, num uint) {
	for getAvailableBits(br) >= 8 && num > 0 {
		dest[0] = byte(getBitsUnmasked(br))
		dropBits(br, 8)

		dest = dest[1:]
		num--
	}

	copy(dest, br.input[br.bytePos:][:num])
	br.bytePos += num
}

func initBitReader(br *bitReader) {
	br.val = 0
	br.bitPos = 64
}

func warmupBitReader(br *bitReader) bool {
	if getAvailableBits(br) == 0 {
		if !pullByte(br) {
			return false
		}
	}

	return true
}
