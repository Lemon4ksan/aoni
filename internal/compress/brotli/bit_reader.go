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

func (br *bitReader) saveState(to *bitReaderState) {
	to.val = br.val
	to.bitPos = br.bitPos
	to.input = br.input
	to.inputLen = br.inputLen
	to.bytePos = br.bytePos
}

func (br *bitReader) restoreState(from *bitReaderState) {
	br.val = from.val
	br.bitPos = from.bitPos
	br.input = from.input
	br.inputLen = from.inputLen
	br.bytePos = from.bytePos
}

func (br *bitReader) availableBits() uint32 {
	return 64 - br.bitPos
}

func (br *bitReader) remainingBytes() uint {
	return uint(uint32(br.inputLen-br.bytePos) + (br.availableBits() >> 3))
}

func (br *bitReader) hasInput(num uint) bool {
	return br.inputLen-br.bytePos >= num
}

func (br *bitReader) fillBitWindow() {
	if br.bitPos >= 32 {
		br.val >>= 32
		br.bitPos ^= 32
		br.val |= (uint64(binary.LittleEndian.Uint32(br.input[br.bytePos:]))) << 32
		br.bytePos += 4
	}
}

func (br *bitReader) pullByte() bool {
	if br.bytePos == br.inputLen {
		return false
	}

	br.val >>= 8
	br.val |= (uint64(br.input[br.bytePos])) << 56
	br.bitPos -= 8
	br.bytePos++

	return true
}

func (br *bitReader) bitsUnmasked() uint64 {
	return br.val >> br.bitPos
}

func (br *bitReader) get16BitsUnmasked() uint32 {
	br.fillBitWindow()
	return uint32(br.bitsUnmasked())
}

func (br *bitReader) getBits(nBits uint32) uint32 {
	br.fillBitWindow()
	return uint32(br.bitsUnmasked()) & bitMask(nBits)
}

func (br *bitReader) safeGetBits(nBits uint32, val *uint32) bool {
	for br.availableBits() < nBits {
		if !br.pullByte() {
			return false
		}
	}

	*val = uint32(br.bitsUnmasked()) & bitMask(nBits)

	return true
}

func (br *bitReader) dropBits(nBits uint32) {
	br.bitPos += nBits
}

func (br *bitReader) unload() {
	unusedBytes := br.availableBits() >> 3
	unusedBits := unusedBytes << 3

	br.bytePos -= uint(unusedBytes)
	if unusedBits == 64 {
		br.val = 0
	} else {
		br.val <<= unusedBits
	}

	br.bitPos += unusedBits
}

func (br *bitReader) takeBits(nBits uint32, val *uint32) {
	*val = uint32(br.bitsUnmasked()) & bitMask(nBits)
	br.dropBits(nBits)
}

func (br *bitReader) readBits(nBits uint32) uint32 {
	var val uint32

	br.fillBitWindow()
	br.takeBits(nBits, &val)

	return val
}

func (br *bitReader) safeReadBits(nBits uint32, val *uint32) bool {
	for br.availableBits() < nBits {
		if !br.pullByte() {
			return false
		}
	}

	br.takeBits(nBits, val)

	return true
}

func (br *bitReader) safeReadBitsMaybeZero(nBits uint32, val *uint32) bool {
	if nBits != 0 {
		return br.safeReadBits(nBits, val)
	}

	*val = 0

	return true
}

func (br *bitReader) jumpToByteBoundary() bool {
	padBitsCount := br.availableBits() & 0x7

	var padBits uint32
	if padBitsCount != 0 {
		br.takeBits(padBitsCount, &padBits)
	}

	return padBits == 0
}

func (br *bitReader) copyBytes(dest []byte, num uint) {
	for br.availableBits() >= 8 && num > 0 {
		dest[0] = byte(br.bitsUnmasked())
		br.dropBits(8)

		dest = dest[1:]
		num--
	}

	copy(dest, br.input[br.bytePos:][:num])
	br.bytePos += num
}

func (br *bitReader) init() {
	br.val = 0
	br.bitPos = 64
}

func (br *bitReader) warmup() bool {
	if br.availableBits() == 0 {
		if !br.pullByte() {
			return false
		}
	}

	return true
}
