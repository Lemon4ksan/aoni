// Copyright 2013 Google Inc. All Rights Reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli

import "math/bits"

const (
	huffmanMaxCodeLength           = 15
	huffmanMaxCodeLengthCodeLength = 5
	huffmanMaxSize26               = 396
	huffmanMaxSize258              = 632
	huffmanMaxSize272              = 646
	reverseBitsMax                 = 8
	reverseBitsLowest              = uint64(1) << (reverseBitsMax - 1)
)

// kMaxHuffmanTableSize defines the maximum possible Huffman lookup table size
// for an alphabet size of (index * 32), max code length 15, and root table bits 8.
var kMaxHuffmanTableSize = []uint16{
	256, 402, 436, 468, 500, 534, 566, 598, 630, 662,
	694, 726, 758, 790, 822, 854, 886, 920, 952, 984,
	1016, 1048, 1080, 1112, 1144, 1176, 1208, 1240, 1272, 1304,
	1336, 1368, 1400, 1432, 1464, 1496, 1528,
}

type huffmanCode struct {
	bits  byte
	value uint16
}

func constructHuffmanCode(bits byte, value uint16) huffmanCode {
	return huffmanCode{bits: bits, value: value}
}

// huffmanTreeGroup represents a group of Huffman trees sharing the same alphabet size.
type huffmanTreeGroup struct {
	htrees       [][]huffmanCode
	codes        []huffmanCode
	alphabetSize uint16
	maxSymbol    uint16
	numHtrees    uint16
}

func reverseBits8(num uint64) uint64 {
	return uint64(bits.Reverse8(byte(num)))
}

func replicateValue(table []huffmanCode, step, end int, code huffmanCode) {
	for {
		end -= step

		table[end] = code
		if end <= 0 {
			break
		}
	}
}

func nextTableBitSize(count []uint16, length, rootBits int) int {
	left := 1 << uint(length-rootBits)
	for length < huffmanMaxCodeLength {
		left -= int(count[length])
		if left <= 0 {
			break
		}

		length++
		left <<= 1
	}

	return length - rootBits
}

func buildCodeLengthsHuffmanTable(table []huffmanCode, codeLengths []byte, count []uint16) {
	var (
		code      huffmanCode
		symbol    int
		key       uint64
		keyStep   uint64
		step      int
		tableSize int
		sorted    [codeLengthCodes]int
		offset    [huffmanMaxCodeLengthCodeLength + 1]int
		bLength   int
		bitsCount int
	)

	symbol = -1

	bLength = 1
	for i := 0; i < huffmanMaxCodeLengthCodeLength; i++ {
		symbol += int(count[bLength])
		offset[bLength] = symbol
		bLength++
	}

	offset[0] = codeLengthCodes - 1
	symbol = codeLengthCodes

	for {
		for i := 0; i < 6; i++ {
			symbol--
			sorted[offset[codeLengths[symbol]]] = symbol
			offset[codeLengths[symbol]]--
		}

		if symbol == 0 {
			break
		}
	}

	tableSize = 1 << huffmanMaxCodeLengthCodeLength

	if offset[0] == 0 {
		code = constructHuffmanCode(0, uint16(sorted[0]))
		for key = 0; key < uint64(tableSize); key++ {
			table[key] = code
		}

		return
	}

	key = 0
	keyStep = reverseBitsLowest
	symbol = 0
	bLength = 1
	step = 2

	for {
		for bitsCount = int(count[bLength]); bitsCount != 0; bitsCount-- {
			code = constructHuffmanCode(byte(bLength), uint16(sorted[symbol]))
			symbol++

			replicateValue(table[reverseBits8(key):], step, tableSize, code)
			key += keyStep
		}

		step <<= 1
		keyStep >>= 1

		bLength++
		if bLength > huffmanMaxCodeLengthCodeLength {
			break
		}
	}
}

func buildHuffmanTable(rootTable []huffmanCode, rootBits int, symbolLists symbolList, count []uint16) uint32 {
	var (
		code       huffmanCode
		table      []huffmanCode
		length     int
		symbol     int
		key        uint64
		keyStep    uint64
		subKey     uint64
		subKeyStep uint64
		step       int
		tableBits  int
		tableSize  int
		totalSize  int
		maxLength  = -1
		bLength    int
		bitsCount  int
	)

	for symbolLists.get(maxLength) == 0xFFFF {
		maxLength--
	}

	maxLength += huffmanMaxCodeLength + 1

	table = rootTable
	tableBits = rootBits
	tableSize = 1 << uint(tableBits)
	totalSize = tableSize

	if tableBits > maxLength {
		tableBits = maxLength
		tableSize = 1 << uint(tableBits)
	}

	key = 0
	keyStep = reverseBitsLowest
	bLength = 1
	step = 2

	for {
		symbol = bLength - (huffmanMaxCodeLength + 1)
		for bitsCount = int(count[bLength]); bitsCount != 0; bitsCount-- {
			symbol = int(symbolLists.get(symbol))
			code = constructHuffmanCode(byte(bLength), uint16(symbol))
			replicateValue(table[reverseBits8(key):], step, tableSize, code)
			key += keyStep
		}

		step <<= 1
		keyStep >>= 1

		bLength++
		if bLength > tableBits {
			break
		}
	}

	for totalSize != tableSize {
		copy(table[tableSize:], table[:uint(tableSize)])
		tableSize <<= 1
	}

	keyStep = reverseBitsLowest >> uint(rootBits-1)
	subKey = reverseBitsLowest << 1
	subKeyStep = reverseBitsLowest
	length = rootBits + 1
	step = 2

	for ; length <= maxLength; length++ {
		symbol = length - (huffmanMaxCodeLength + 1)
		for ; count[length] != 0; count[length]-- {
			if subKey == reverseBitsLowest<<1 {
				table = table[tableSize:]
				tableBits = nextTableBitSize(count, length, rootBits)
				tableSize = 1 << uint(tableBits)
				totalSize += tableSize
				subKey = reverseBits8(key)
				key += keyStep
				rootTable[subKey] = constructHuffmanCode(
					byte(tableBits+rootBits),
					uint16(uint64(uint(-cap(table)+cap(rootTable)))-subKey),
				)
				subKey = 0
			}

			symbol = int(symbolLists.get(symbol))
			code = constructHuffmanCode(byte(length-rootBits), uint16(symbol))
			replicateValue(table[reverseBits8(subKey):], step, tableSize, code)
			subKey += subKeyStep
		}

		step <<= 1
		subKeyStep >>= 1
	}

	return uint32(totalSize)
}

func buildSimpleHuffmanTable(table []huffmanCode, rootBits int, val []uint16, numSymbols uint32) uint32 {
	tableSize := 1
	goalSize := 1 << uint(rootBits)

	switch numSymbols {
	case 0:
		table[0] = constructHuffmanCode(0, val[0])

	case 1:
		if val[1] > val[0] {
			table[0] = constructHuffmanCode(1, val[0])
			table[1] = constructHuffmanCode(1, val[1])
		} else {
			table[0] = constructHuffmanCode(1, val[1])
			table[1] = constructHuffmanCode(1, val[0])
		}

		tableSize = 2

	case 2:
		table[0] = constructHuffmanCode(1, val[0])

		table[2] = constructHuffmanCode(1, val[0])
		if val[2] > val[1] {
			table[1] = constructHuffmanCode(2, val[1])
			table[3] = constructHuffmanCode(2, val[2])
		} else {
			table[1] = constructHuffmanCode(2, val[2])
			table[3] = constructHuffmanCode(2, val[1])
		}

		tableSize = 4

	case 3:
		if val[0] > val[1] {
			val[0], val[1] = val[1], val[0]
		}

		if val[2] > val[3] {
			val[2], val[3] = val[3], val[2]
		}

		if val[0] > val[2] {
			val[0], val[2] = val[2], val[0]
		}

		if val[1] > val[3] {
			val[1], val[3] = val[3], val[1]
		}

		if val[1] > val[2] {
			val[1], val[2] = val[2], val[1]
		}

		table[0] = constructHuffmanCode(2, val[0])
		table[2] = constructHuffmanCode(2, val[1])
		table[1] = constructHuffmanCode(2, val[2])
		table[3] = constructHuffmanCode(2, val[3])
		tableSize = 4

	case 4:
		if val[3] < val[2] {
			val[3], val[2] = val[2], val[3]
		}

		table[0] = constructHuffmanCode(1, val[0])
		table[2] = constructHuffmanCode(1, val[0])
		table[4] = constructHuffmanCode(1, val[0])
		table[6] = constructHuffmanCode(1, val[0])
		table[1] = constructHuffmanCode(2, val[1])
		table[5] = constructHuffmanCode(2, val[1])
		table[3] = constructHuffmanCode(3, val[2])
		table[7] = constructHuffmanCode(3, val[3])
		tableSize = 8
	}

	for tableSize != goalSize {
		copy(table[tableSize:], table[:uint(tableSize)])
		tableSize <<= 1
	}

	return uint32(goalSize)
}

const (
	huffmanTableBits = 8
	huffmanTableMask = 0xFF
)

/*
Decodes the Huffman code.
This method doesn't read data from the bit reader, BUT drops the amount of
bits that correspond to the decoded symbol.
bits MUST contain at least 15 (BROTLI_HUFFMAN_MAX_CODE_LENGTH) valid bits.
*/
func decodeSymbol(bits uint32, table []huffmanCode, br *bitReader) uint32 {
	entry := table[bits&huffmanTableMask]
	if entry.bits > huffmanTableBits {
		nbits := uint32(entry.bits) - huffmanTableBits
		br.dropBits(huffmanTableBits)
		entry = table[uint32(entry.value)+((bits>>huffmanTableBits)&bitMask(nbits))]
	}

	br.dropBits(uint32(entry.bits))

	return uint32(entry.value)
}

/*
Reads and decodes the next Huffman code from bit-stream.
This method peeks 16 bits of input and drops 0 - 15 of them.
*/
func readSymbol(table []huffmanCode, br *bitReader) uint32 {
	return decodeSymbol(br.get16BitsUnmasked(), table, br)
}

/*
Same as decodeSymbol, but it is known that there is less than 15 bits of
input are currently available.
*/
func safeDecodeSymbol(table []huffmanCode, br *bitReader, result *uint32) bool {
	availBits := br.availableBits()

	if availBits == 0 {
		if table[0].bits == 0 {
			*result = uint32(table[0].value)
			return true
		}

		return false /* No valid bits at all. */
	}

	val := uint32(br.bitsUnmasked())

	entry := table[val&huffmanTableMask]
	if entry.bits <= huffmanTableBits {
		if uint32(entry.bits) <= availBits {
			br.dropBits(uint32(entry.bits))
			*result = uint32(entry.value)
			return true
		}

		return false /* Not enough bits for the first level. */
	}

	if availBits <= huffmanTableBits {
		return false /* Not enough bits to move to the second level. */
	}

	/* Speculatively drop HUFFMAN_TABLE_BITS. */
	val = (val & bitMask(uint32(entry.bits))) >> huffmanTableBits
	availBits -= huffmanTableBits

	subTable := table[uint32(entry.value)+val:]
	if availBits < uint32(subTable[0].bits) {
		return false /* Not enough bits for the second level. */
	}

	br.dropBits(huffmanTableBits + uint32(subTable[0].bits))
	*result = uint32(subTable[0].value)

	return true
}

func safeReadSymbol(table []huffmanCode, br *bitReader, result *uint32) bool {
	var val uint32
	if br.safeGetBits(15, &val) {
		*result = decodeSymbol(val, table, br)
		return true
	}

	return safeDecodeSymbol(table, br, result)
}

/* Makes a look-up in first level Huffman table. Peeks 8 bits. */
func preloadSymbol(safe int, table []huffmanCode, br *bitReader, bits, value *uint32) {
	if safe != 0 {
		return
	}

	entry := table[br.getBits(huffmanTableBits)]
	*bits = uint32(entry.bits)
	*value = uint32(entry.value)
}

/*
Decodes the next Huffman code using data prepared by PreloadSymbol.
Reads 0 - 15 bits. Also peeks 8 following bits.
*/
func readPreloadedSymbol(table []huffmanCode, br *bitReader, bits, value *uint32) uint32 {
	result := *value

	if *bits > huffmanTableBits {
		val := br.get16BitsUnmasked()
		offset := val & huffmanTableMask
		mask := bitMask(*bits - huffmanTableBits)
		br.dropBits(huffmanTableBits)
		extIdx := offset + *value + ((val >> huffmanTableBits) & mask)
		ext := table[extIdx]
		br.dropBits(uint32(ext.bits))
		result = uint32(ext.value)
	} else {
		br.dropBits(*bits)
	}

	preloadSymbol(0, table, br, bits, value)

	return result
}
