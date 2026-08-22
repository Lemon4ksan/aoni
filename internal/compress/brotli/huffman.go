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
		for i := 0; i < 3; i++ {
			for k := i + 1; k < 4; k++ {
				if val[k] < val[i] {
					val[k], val[i] = val[i], val[k]
				}
			}
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
