// Copyright 2016 Google Inc. All Rights Reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli

import "math/bits"

var kCodeLengthCodeOrder = [codeLengthCodes]byte{1, 2, 3, 4, 0, 5, 17, 6, 16, 7, 8, 9, 10, 11, 12, 13, 14, 15}

/* Static prefix code for the complex code length code lengths. */
var kCodeLengthPrefixLength = [16]byte{2, 2, 2, 3, 2, 2, 2, 4, 2, 2, 2, 3, 2, 2, 2, 4}

var kCodeLengthPrefixValue = [16]byte{0, 4, 3, 2, 0, 4, 3, 1, 0, 4, 3, 2, 0, 4, 3, 5}

/* Decodes a metablock length and flags by reading 2 - 31 bits. */
func (s *Reader) decodeMetaBlockLength() int {
	br := &s.br

	var (
		bits uint32
		i    int
	)

	for {
		switch s.substateMetablockHeader {
		case stateMetablockHeaderNone:
			if !br.safeReadBits(1, &bits) {
				return decoderNeedsMoreInput
			}

			s.isLastMetablock = bits != 0
			s.metaBlockRemainingLen = 0
			s.isUncompressed = false
			s.isMetadata = false

			if !s.isLastMetablock {
				s.substateMetablockHeader = stateMetablockHeaderNibbles
				break
			}

			s.substateMetablockHeader = stateMetablockHeaderEmpty

			fallthrough

		case stateMetablockHeaderEmpty:
			if !br.safeReadBits(1, &bits) {
				return decoderNeedsMoreInput
			}

			if bits != 0 {
				s.substateMetablockHeader = stateMetablockHeaderNone
				return decoderSuccess
			}

			s.substateMetablockHeader = stateMetablockHeaderNibbles

			fallthrough

		case stateMetablockHeaderNibbles:
			if !br.safeReadBits(2, &bits) {
				return decoderNeedsMoreInput
			}

			s.sizeNibbles = byte(bits + 4)

			s.loopCounter = 0
			if bits == 3 {
				s.isMetadata = true
				s.substateMetablockHeader = stateMetablockHeaderReserved
				break
			}

			s.substateMetablockHeader = stateMetablockHeaderSize

			fallthrough

		case stateMetablockHeaderSize:
			i = s.loopCounter

			for ; i < int(s.sizeNibbles); i++ {
				if !br.safeReadBits(4, &bits) {
					s.loopCounter = i
					return decoderNeedsMoreInput
				}

				if byte(i+1) == s.sizeNibbles && s.sizeNibbles > 4 && bits == 0 {
					return decoderErrorFormatExuberantNibble
				}

				s.metaBlockRemainingLen |= int(bits << uint(i*4))
			}

			s.substateMetablockHeader = stateMetablockHeaderUncompressed

			fallthrough

		case stateMetablockHeaderUncompressed:
			if !s.isLastMetablock {
				if !br.safeReadBits(1, &bits) {
					return decoderNeedsMoreInput
				}

				s.isUncompressed = bits != 0
			}

			s.metaBlockRemainingLen++
			s.substateMetablockHeader = stateMetablockHeaderNone

			return decoderSuccess

		case stateMetablockHeaderReserved:
			if !br.safeReadBits(1, &bits) {
				return decoderNeedsMoreInput
			}

			if bits != 0 {
				return decoderErrorFormatReserved
			}

			s.substateMetablockHeader = stateMetablockHeaderBytes

			fallthrough

		case stateMetablockHeaderBytes:
			if !br.safeReadBits(2, &bits) {
				return decoderNeedsMoreInput
			}

			if bits == 0 {
				s.substateMetablockHeader = stateMetablockHeaderNone
				return decoderSuccess
			}

			s.sizeNibbles = byte(bits)
			s.substateMetablockHeader = stateMetablockHeaderMetadata

			fallthrough

		case stateMetablockHeaderMetadata:
			i = s.loopCounter

			for ; i < int(s.sizeNibbles); i++ {
				if !br.safeReadBits(8, &bits) {
					s.loopCounter = i
					return decoderNeedsMoreInput
				}

				if byte(i+1) == s.sizeNibbles && s.sizeNibbles > 1 && bits == 0 {
					return decoderErrorFormatExuberantMetaNibble
				}

				s.metaBlockRemainingLen |= int(bits << uint(i*8))
			}

			s.metaBlockRemainingLen++
			s.substateMetablockHeader = stateMetablockHeaderNone

			return decoderSuccess

		default:
			return decoderErrorUnreachable
		}
	}
}

/*
Reads (s->symbol + 1) symbols.

	Totally 1..4 symbols are read, 1..11 bits each.
	The list of symbols MUST NOT contain duplicates.
*/
func (s *Reader) readSimpleHuffmanSymbols(alphabetSize, maxSymbol uint32) int {
	br := &s.br
	maxBits := uint32(bits.Len32(alphabetSize - 1))
	i := s.subLoopCounter

	numSymbols := s.symbol
	for i <= numSymbols {
		var v uint32
		if !br.safeReadBits(maxBits, &v) {
			s.subLoopCounter = i
			s.substateHuffman = stateHuffmanSimpleRead
			return decoderNeedsMoreInput
		}

		if v >= maxSymbol {
			return decoderErrorFormatSimpleHuffmanAlphabet
		}

		s.symbolsListsArray[i] = uint16(v)
		i++
	}

	for i = 0; i < numSymbols; i++ {
		for k := i + 1; k <= numSymbols; k++ {
			if s.symbolsListsArray[i] == s.symbolsListsArray[k] {
				return decoderErrorFormatSimpleHuffmanSame
			}
		}
	}

	return decoderSuccess
}

/*
processSingleCodeLength decodes a single symbol code length:

	A) resets the repeat variable
	B) remembers code length (if not 0)
	C) extends corresponding index-chain
	D) reduces the Huffman space
	E) updates the histogram
*/
func (s *Reader) processSingleCodeLength(codeLen uint32) {
	s.repeat = 0

	if codeLen != 0 { /* codeLen == 1..15 */
		s.symbolLists.put(s.nextSymbol[codeLen], uint16(s.symbol))
		s.nextSymbol[codeLen] = int(s.symbol)
		s.prevCodeLen = codeLen
		s.space -= 32768 >> codeLen
		s.codeLengthHisto[codeLen]++
	}

	s.symbol++
}

/*
processRepeatedCodeLength processes repeated symbol code lengths.
*/
func (s *Reader) processRepeatedCodeLength(codeLen, repeatDelta, alphabetSize uint32) {
	var (
		extraBits uint32 = 3
		newLen    uint32 = 0
	)

	if codeLen == repeatPreviousCodeLength {
		newLen = s.prevCodeLen
		extraBits = 2
	}

	if s.repeatCodeLen != newLen {
		s.repeat = 0
		s.repeatCodeLen = newLen
	}

	oldRepeat := s.repeat
	if s.repeat > 0 {
		s.repeat -= 2
		s.repeat <<= extraBits
	}

	s.repeat += repeatDelta + 3

	repeatDelta = s.repeat - oldRepeat
	if s.symbol+repeatDelta > alphabetSize {
		s.symbol = alphabetSize
		s.space = 0xFFFFF
		return
	}

	if s.repeatCodeLen != 0 {
		last := uint(s.symbol + repeatDelta)
		next := s.nextSymbol[s.repeatCodeLen]

		for {
			s.symbolLists.put(next, uint16(s.symbol))
			next = int(s.symbol)

			s.symbol++
			if s.symbol == uint32(last) {
				break
			}
		}

		s.nextSymbol[s.repeatCodeLen] = next
		s.space -= repeatDelta << (15 - s.repeatCodeLen)
		s.codeLengthHisto[s.repeatCodeLen] += uint16(repeatDelta)
	} else {
		s.symbol += repeatDelta
	}
}

/* Reads and decodes symbol codelengths. */
func (s *Reader) readSymbolCodeLengths(alphabetSize uint32) int {
	br := &s.br

	if !br.warmup() {
		return decoderNeedsMoreInput
	}

	for s.symbol < alphabetSize && s.space > 0 {
		if !br.hasInput(shortFillBitWindowRead) {
			return decoderNeedsMoreInput
		}

		br.fillBitWindow()
		p := s.table[br.bitsUnmasked()&uint64(bitMask(huffmanMaxCodeLengthCodeLength)):]
		br.dropBits(uint32(p[0].bits))

		codeLen := uint32(p[0].value)
		if codeLen < repeatPreviousCodeLength {
			s.processSingleCodeLength(codeLen)
		} else {
			var extraBits uint32
			if codeLen == repeatPreviousCodeLength {
				extraBits = 2
			} else {
				extraBits = 3
			}

			repeatDelta := uint32(br.bitsUnmasked()) & bitMask(extraBits)
			br.dropBits(extraBits)
			s.processRepeatedCodeLength(codeLen, repeatDelta, alphabetSize)
		}
	}

	return decoderSuccess
}

func (s *Reader) safeReadSymbolCodeLengths(alphabetSize uint32) int {
	br := &s.br
	getByte := false

	for s.symbol < alphabetSize && s.space > 0 {
		var bits uint32

		if getByte && !br.pullByte() {
			return decoderNeedsMoreInput
		}

		getByte = false

		availableBits := br.availableBits()
		if availableBits != 0 {
			bits = uint32(br.bitsUnmasked())
		}

		p := s.table[bits&bitMask(huffmanMaxCodeLengthCodeLength):]
		if uint32(p[0].bits) > availableBits {
			getByte = true
			continue
		}

		codeLen := uint32(p[0].value)
		if codeLen < repeatPreviousCodeLength {
			br.dropBits(uint32(p[0].bits))
			s.processSingleCodeLength(codeLen)
		} else {
			extraBits := codeLen - 14
			repeatDelta := (bits >> p[0].bits) & bitMask(extraBits)

			if availableBits < uint32(p[0].bits)+extraBits {
				getByte = true
				continue
			}

			br.dropBits(uint32(p[0].bits) + extraBits)
			s.processRepeatedCodeLength(codeLen, repeatDelta, alphabetSize)
		}
	}

	return decoderSuccess
}

/*
Reads and decodes 15..18 codes using static prefix code.
Each code is 2..4 bits long. In total 30..72 bits are used.
*/
func (s *Reader) readCodeLengthCodeLengths() int {
	br := &s.br
	numCodes := s.repeat
	space := s.space
	i := s.subLoopCounter

	for ; i < codeLengthCodes; i++ {
		var (
			codeLenIdx = kCodeLengthCodeOrder[i]
			ix         uint32
			v          uint32
		)

		if !br.safeGetBits(4, &ix) {
			availableBits := br.availableBits()
			if availableBits != 0 {
				ix = uint32(br.bitsUnmasked() & 0xF)
			} else {
				ix = 0
			}

			if uint32(kCodeLengthPrefixLength[ix]) > availableBits {
				s.subLoopCounter = i
				s.repeat = numCodes
				s.space = space
				s.substateHuffman = stateHuffmanComplex

				return decoderNeedsMoreInput
			}
		}

		v = uint32(kCodeLengthPrefixValue[ix])
		br.dropBits(uint32(kCodeLengthPrefixLength[ix]))

		s.codeLengthCodeLengths[codeLenIdx] = byte(v)
		if v != 0 {
			space -= 32 >> v
			numCodes++
			s.codeLengthHisto[v]++

			if space-1 >= 32 {
				/* space is 0 or wrapped around. */
				break
			}
		}
	}

	if numCodes != 1 && space != 0 {
		return decoderErrorFormatClSpace
	}

	return decoderSuccess
}

/*
Decodes the Huffman tables.
*/
func (s *Reader) readHuffmanCode(alphabetSize, maxSymbol uint32, table []huffmanCode, optTableSize *uint32) int {
	br := &s.br

	/* Unnecessary masking, but might be good for safety. */
	alphabetSize &= 0x7FF

	/* State machine. */
	for {
		switch s.substateHuffman {
		case stateHuffmanNone:
			if !br.safeReadBits(2, &s.subLoopCounter) {
				return decoderNeedsMoreInput
			}

			/* The value is used as follows:
			   1 for simple code;
			   0 for no skipping, 2 skips 2 code lengths, 3 skips 3 code lengths */
			if s.subLoopCounter != 1 {
				s.space = 32
				s.repeat = 0 /* num_codes */

				for i := 0; i <= huffmanMaxCodeLengthCodeLength; i++ {
					s.codeLengthHisto[i] = 0
				}

				for i := 0; i < codeLengthCodes; i++ {
					s.codeLengthCodeLengths[i] = 0
				}

				s.substateHuffman = stateHuffmanComplex

				continue
			}

			fallthrough

			/* Read symbols, codes & code lengths directly. */

		case stateHuffmanSimpleSize:
			if !br.safeReadBits(2, &s.symbol) { /* num_symbols */
				s.substateHuffman = stateHuffmanSimpleSize
				return decoderNeedsMoreInput
			}

			s.subLoopCounter = 0

			fallthrough

		case stateHuffmanSimpleRead:
			result := s.readSimpleHuffmanSymbols(alphabetSize, maxSymbol)
			if result != decoderSuccess {
				return result
			}

			fallthrough

		case stateHuffmanSimpleBuild:
			if s.symbol == 3 {
				var bits uint32
				if !br.safeReadBits(1, &bits) {
					s.substateHuffman = stateHuffmanSimpleBuild
					return decoderNeedsMoreInput
				}

				s.symbol += bits
			}

			tableSize := buildSimpleHuffmanTable(table, huffmanTableBits, s.symbolsListsArray[:], s.symbol)
			if optTableSize != nil {
				*optTableSize = tableSize
			}

			s.substateHuffman = stateHuffmanNone

			return decoderSuccess

			/* Decode Huffman-coded code lengths. */

		case stateHuffmanComplex:
			result := s.readCodeLengthCodeLengths()
			if result != decoderSuccess {
				return result
			}

			buildCodeLengthsHuffmanTable(s.table[:], s.codeLengthCodeLengths[:], s.codeLengthHisto[:])

			for i := 0; i < 16; i++ {
				s.codeLengthHisto[i] = 0
			}

			for i := 0; i <= huffmanMaxCodeLength; i++ {
				s.nextSymbol[i] = int(i) - (huffmanMaxCodeLength + 1)
				s.symbolLists.put(s.nextSymbol[i], 0xFFFF)
			}

			s.symbol = 0
			s.prevCodeLen = initialRepeatedCodeLength
			s.repeat = 0
			s.repeatCodeLen = 0
			s.space = 32768
			s.substateHuffman = stateHuffmanLengthSymbols

			fallthrough

		case stateHuffmanLengthSymbols:
			result := s.readSymbolCodeLengths(maxSymbol)
			if result == decoderNeedsMoreInput {
				result = s.safeReadSymbolCodeLengths(maxSymbol)
			}

			if result != decoderSuccess {
				return result
			}

			if s.space != 0 {
				return decoderErrorFormatHuffmanSpace
			}

			tableSize := buildHuffmanTable(table, huffmanTableBits, s.symbolLists, s.codeLengthHisto[:])
			if optTableSize != nil {
				*optTableSize = tableSize
			}

			s.substateHuffman = stateHuffmanNone

			return decoderSuccess

		default:
			return decoderErrorUnreachable
		}
	}
}

/* Decodes a block length by reading 3..39 bits. */
func (s *Reader) readBlockLength(table []huffmanCode) uint32 {
	code := readSymbol(table, &s.br)
	nbits := kBlockLengthPrefixCode[code].nbits /* nbits == 2..24 */

	return kBlockLengthPrefixCode[code].offset + s.br.readBits(nbits)
}

/*
WARNING: if state is not BROTLI_STATE_READ_BLOCK_LENGTH_NONE, then
reading can't be continued with readBlockLength.
*/
func (s *Reader) safeReadBlockLength(result *uint32, table []huffmanCode) bool {
	var index uint32
	if s.substateReadBlockLength == stateReadBlockLengthNone {
		if !safeReadSymbol(table, &s.br, &index) {
			return false
		}
	} else {
		index = s.blockLengthIndex
	}

	var bits uint32 /* nbits == 2..24 */

	nbits := kBlockLengthPrefixCode[index].nbits
	if !s.br.safeReadBits(nbits, &bits) {
		s.blockLengthIndex = index
		s.substateReadBlockLength = stateReadBlockLengthSuffix
		return false
	}

	*result = kBlockLengthPrefixCode[index].offset + bits
	s.substateReadBlockLength = stateReadBlockLengthNone

	return true
}

/*
Transform:

 1. initialize list L with values 0, 1,... 255

 2. For each input element X:
    2.1) let Y = L[X]
    2.2) remove X-th element from L
    2.3) prepend Y to L
    2.4) append Y to output

    In most cases max(Y) <= 7, so most of L remains intact.
    To reduce the cost of initialization, we reuse L, remember the upper bound
    of Y values, and reinitialize only first elements in L.

    Most of input values are 0 and 1. To reduce number of branches, we replace
    inner for loop with do-while.
*/
func inverseMoveToFrontTransform(v []byte, vLen uint32) {
	var mtf [256]byte
	for i := 1; i < 256; i++ {
		mtf[i] = byte(i)
	}

	var mtf1 byte

	/* Transform the input. */
	for i := 0; uint32(i) < vLen; i++ {
		index := int(v[i])
		value := mtf[index]

		v[i] = value
		mtf1 = value

		for index >= 1 {
			index--
			mtf[index+1] = mtf[index]
		}

		mtf[0] = mtf1
	}
}

/* Decodes a series of Huffman table using ReadHuffmanCode function. */
func (s *Reader) decodeHuffmanTreeGroup(group *huffmanTreeGroup) int {
	if s.substateTreeGroup != stateTreeGroupLoop {
		s.next = group.codes
		s.htreeIndex = 0
		s.substateTreeGroup = stateTreeGroupLoop
	}

	for s.htreeIndex < int(group.numHtrees) {
		var tableSize uint32

		result := s.readHuffmanCode(
			uint32(group.alphabetSize),
			uint32(group.maxSymbol),
			s.next,
			&tableSize,
		)

		if result != decoderSuccess {
			return result
		}

		group.htrees[s.htreeIndex] = s.next
		s.next = s.next[tableSize:]
		s.htreeIndex++
	}

	s.substateTreeGroup = stateTreeGroupNone

	return decoderSuccess
}

/*
Decodes a context map.

	Decoding is done in 4 phases:
	 1) Read auxiliary information (6..16 bits) and allocate memory.
	    In case of trivial context map, decoding is finished at this phase.
	 2) Decode Huffman table using ReadHuffmanCode function.
	    This table will be used for reading context map items.
	 3) Read context map items; "0" values could be run-length encoded.
	 4) Optionally, apply InverseMoveToFront transform to the resulting map.
*/
func (s *Reader) decodeContextMap(contextMapSize uint32, numHtrees *uint32, contextMapArg *[]byte) int {
	br := &s.br

	switch s.substateContextMap {
	case stateContextMapNone:
		result := s.decodeVarLenUint8(numHtrees)
		if result != decoderSuccess {
			return result
		}

		(*numHtrees)++
		s.contextIndex = 0

		*contextMapArg = make([]byte, uint(contextMapSize))
		if *contextMapArg == nil {
			return decoderErrorAllocContextMap
		}

		if *numHtrees <= 1 {
			for i := 0; i < int(contextMapSize); i++ {
				(*contextMapArg)[i] = 0
			}

			return decoderSuccess
		}

		s.substateContextMap = stateContextMapReadPrefix

		fallthrough

	case stateContextMapReadPrefix:
		var bits uint32

		/* In next stage ReadHuffmanCode uses at least 4 bits, so it is safe to peek 4 bits ahead. */
		if !br.safeGetBits(5, &bits) {
			return decoderNeedsMoreInput
		}

		if bits&1 != 0 { /* Use RLE for zeros. */
			s.maxRunLengthPrefix = (bits >> 1) + 1

			br.dropBits(5)
		} else {
			s.maxRunLengthPrefix = 0

			br.dropBits(1)
		}

		s.substateContextMap = stateContextMapHuffman

		fallthrough

	case stateContextMapHuffman:
		alphabetSize := *numHtrees + s.maxRunLengthPrefix

		result := s.readHuffmanCode(alphabetSize, alphabetSize, s.contextMapTable[:], nil)
		if result != decoderSuccess {
			return result
		}

		s.code = 0xFFFF
		s.substateContextMap = stateContextMapDecode

		fallthrough

	case stateContextMapDecode:
		contextIndex := s.contextIndex
		maxRunLengthPrefix := s.maxRunLengthPrefix
		contextMap := *contextMapArg
		code := s.code
		skipPreamble := (code != 0xFFFF)

		for contextIndex < contextMapSize || skipPreamble {
			if !skipPreamble {
				if !safeReadSymbol(s.contextMapTable[:], br, &code) {
					s.code = 0xFFFF
					s.contextIndex = contextIndex
					return decoderNeedsMoreInput
				}

				if code == 0 {
					contextMap[contextIndex] = 0
					contextIndex++
					continue
				}

				if code > maxRunLengthPrefix {
					contextMap[contextIndex] = byte(code - maxRunLengthPrefix)
					contextIndex++
					continue
				}
			} else {
				skipPreamble = false
			}

			/* RLE sub-stage. */
			var reps uint32
			if !br.safeReadBits(code, &reps) {
				s.code = code
				s.contextIndex = contextIndex
				return decoderNeedsMoreInput
			}

			reps += 1 << code
			if contextIndex+reps > contextMapSize {
				return decoderErrorFormatContextMapRepeat
			}

			clear(contextMap[contextIndex : contextIndex+reps])
			contextIndex += reps
		}

		fallthrough

	case stateContextMapTransform:
		var bits uint32
		if !br.safeReadBits(1, &bits) {
			s.substateContextMap = stateContextMapTransform
			return decoderNeedsMoreInput
		}

		if bits != 0 {
			inverseMoveToFrontTransform(*contextMapArg, contextMapSize)
		}

		s.substateContextMap = stateContextMapNone

		return decoderSuccess

	default:
		return decoderErrorUnreachable
	}
}

/*
Decodes a command or literal and updates block type ring-buffer.
Reads 3..54 bits.
*/
func (s *Reader) decodeBlockTypeAndLength(safe, treeType int) bool {
	maxBlockType := s.numBlockTypes[treeType]

	typeTree := s.blockTypeTrees[treeType*huffmanMaxSize258:]
	lenTree := s.blockLenTrees[treeType*huffmanMaxSize26:]

	br := &s.br
	ringbuffer := s.blockTypeRB[treeType*2:]

	var blockType uint32

	if maxBlockType <= 1 {
		return false
	}

	/* Read 0..15 + 3..39 bits. */
	if safe == 0 {
		blockType = readSymbol(typeTree, br)
		s.blockLength[treeType] = s.readBlockLength(lenTree)
	} else {
		var memento bitReaderState
		br.saveState(&memento)

		if !safeReadSymbol(typeTree, br, &blockType) {
			return false
		}

		if !s.safeReadBlockLength(&s.blockLength[treeType], lenTree) {
			s.substateReadBlockLength = stateReadBlockLengthNone

			br.restoreState(&memento)

			return false
		}
	}

	switch blockType {
	case 1:
		blockType = ringbuffer[1] + 1
	case 0:
		blockType = ringbuffer[0]
	default:
		blockType -= 2
	}

	if blockType >= maxBlockType {
		blockType -= maxBlockType
	}

	ringbuffer[0] = ringbuffer[1]
	ringbuffer[1] = blockType

	return true
}

/* Reads 1..256 2-bit context modes. */
func (s *Reader) readContextModes() int {
	br := &s.br
	i := s.loopCounter

	for i < int(s.numBlockTypes[0]) {
		var bits uint32
		if !br.safeReadBits(2, &bits) {
			s.loopCounter = i
			return decoderNeedsMoreInput
		}

		s.contextModes[i] = byte(bits)
		i++
	}

	return decoderSuccess
}
