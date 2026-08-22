// Copyright 2016 Google Inc. All Rights Reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli

import "math/bits"

const (
	decoderResultError           = 0
	decoderResultSuccess         = 1
	decoderResultNeedsMoreInput  = 2
	decoderResultNeedsMoreOutput = 3
)

/**
 * Error code for detailed logging / production debugging.
 *
 * See ::BrotliDecoderGetErrorCode and ::BROTLI_LAST_ERROR_CODE.
 */
const (
	decoderNoError                          = 0
	decoderSuccess                          = 1
	decoderNeedsMoreInput                   = 2
	decoderNeedsMoreOutput                  = 3
	decoderErrorFormatExuberantNibble       = -1
	decoderErrorFormatReserved              = -2
	decoderErrorFormatExuberantMetaNibble   = -3
	decoderErrorFormatSimpleHuffmanAlphabet = -4
	decoderErrorFormatSimpleHuffmanSame     = -5
	decoderErrorFormatClSpace               = -6
	decoderErrorFormatHuffmanSpace          = -7
	decoderErrorFormatContextMapRepeat      = -8
	decoderErrorFormatBlockLength1          = -9
	decoderErrorFormatBlockLength2          = -10
	decoderErrorFormatTransform             = -11
	decoderErrorFormatDictionary            = -12
	decoderErrorFormatWindowBits            = -13
	decoderErrorFormatPadding1              = -14
	decoderErrorFormatPadding2              = -15
	decoderErrorFormatDistance              = -16
	decoderErrorDictionaryNotSet            = -19
	decoderErrorInvalidArguments            = -20
	decoderErrorAllocContextModes           = -21
	decoderErrorAllocTreeGroups             = -22
	decoderErrorAllocContextMap             = -25
	decoderErrorAllocRingBuffer1            = -26
	decoderErrorAllocRingBuffer2            = -27
	decoderErrorAllocBlockTypeTrees         = -30
	decoderErrorUnreachable                 = -31
)

const huffmanTableBits = 8

const huffmanTableMask = 0xFF

/*
We need the slack region for the following reasons:
  - doing up to two 16-byte copies for fast backward copying
  - inserting transformed dictionary word (5 prefix + 24 base + 8 suffix)
*/
const kRingBufferWriteAheadSlack uint32 = 42

var kCodeLengthCodeOrder = [codeLengthCodes]byte{1, 2, 3, 4, 0, 5, 17, 6, 16, 7, 8, 9, 10, 11, 12, 13, 14, 15}

/* Static prefix code for the complex code length code lengths. */
var kCodeLengthPrefixLength = [16]byte{2, 2, 2, 3, 2, 2, 2, 4, 2, 2, 2, 3, 2, 2, 2, 4}

var kCodeLengthPrefixValue = [16]byte{0, 4, 3, 2, 0, 4, 3, 1, 0, 4, 3, 2, 0, 4, 3, 5}

/* Saves error code and converts it to BrotliDecoderResult. */
func (s *Reader) saveErrorCode(e int) int {
	s.errorCode = e
	switch e {
	case decoderSuccess:
		return decoderResultSuccess

	case decoderNeedsMoreInput:
		return decoderResultNeedsMoreInput

	case decoderNeedsMoreOutput:
		return decoderResultNeedsMoreOutput

	default:
		return decoderResultError
	}
}

/*
Decodes WBITS by reading 1 - 7 bits, or 0x11 for "Large Window Brotli".
Precondition: bit-reader accumulator has at least 8 bits.
*/
func (s *Reader) decodeWindowBits() int {
	var n uint32

	largeWindow := s.largeWindow
	s.largeWindow = false

	s.br.takeBits(1, &n)

	if n == 0 {
		s.windowBits = 16
		return decoderSuccess
	}

	s.br.takeBits(3, &n)

	if n != 0 {
		s.windowBits = 17 + n
		return decoderSuccess
	}

	s.br.takeBits(3, &n)

	if n == 1 {
		if largeWindow {
			s.br.takeBits(1, &n)

			if n == 1 {
				return decoderErrorFormatWindowBits
			}

			s.largeWindow = true

			return decoderSuccess
		}

		return decoderErrorFormatWindowBits
	}

	if n != 0 {
		s.windowBits = 8 + n
		return decoderSuccess
	}

	s.windowBits = 17

	return decoderSuccess
}

/* Decodes a number in the range [0..255], by reading 1 - 11 bits. */
func (s *Reader) decodeVarLenUint8(value *uint32) int {
	br := &s.br

	var bits uint32
	switch s.substateDecodeUint8 {
	case stateDecodeUint8None:
		if !br.safeReadBits(1, &bits) {
			return decoderNeedsMoreInput
		}

		if bits == 0 {
			*value = 0
			return decoderSuccess
		}

		fallthrough

	case stateDecodeUint8Short:
		if !br.safeReadBits(3, &bits) {
			s.substateDecodeUint8 = stateDecodeUint8Short
			return decoderNeedsMoreInput
		}

		if bits == 0 {
			*value = 1
			s.substateDecodeUint8 = stateDecodeUint8None
			return decoderSuccess
		}

		/* Use output value as a temporary storage. It MUST be persisted. */
		*value = bits

		fallthrough

	case stateDecodeUint8Long:
		if !br.safeReadBits(*value, &bits) {
			s.substateDecodeUint8 = stateDecodeUint8Long
			return decoderNeedsMoreInput
		}

		*value = (1 << *value) + bits
		s.substateDecodeUint8 = stateDecodeUint8None

		return decoderSuccess

	default:
		return decoderErrorUnreachable
	}
}

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

			if bits != 0 {
				s.isLastMetablock = 1
			} else {
				s.isLastMetablock = 0
			}

			s.metaBlockRemainingLen = 0
			s.isUncompressed = 0

			s.isMetadata = 0
			if s.isLastMetablock == 0 {
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

			s.sizeNibbles = uint(byte(bits + 4))

			s.loopCounter = 0
			if bits == 3 {
				s.isMetadata = 1
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

				if uint(i+1) == s.sizeNibbles && s.sizeNibbles > 4 && bits == 0 {
					return decoderErrorFormatExuberantNibble
				}

				s.metaBlockRemainingLen |= int(bits << uint(i*4))
			}

			s.substateMetablockHeader = stateMetablockHeaderUncompressed

			fallthrough

		case stateMetablockHeaderUncompressed:
			if s.isLastMetablock == 0 {
				if !br.safeReadBits(1, &bits) {
					return decoderNeedsMoreInput
				}

				if bits != 0 {
					s.isUncompressed = 1
				} else {
					s.isUncompressed = 0
				}
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

			s.sizeNibbles = uint(byte(bits))
			s.substateMetablockHeader = stateMetablockHeaderMetadata

			fallthrough

		case stateMetablockHeaderMetadata:
			i = s.loopCounter

			for ; i < int(s.sizeNibbles); i++ {
				if !br.safeReadBits(8, &bits) {
					s.loopCounter = i
					return decoderNeedsMoreInput
				}

				if uint(i+1) == s.sizeNibbles && s.sizeNibbles > 1 && bits == 0 {
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
Decodes the Huffman code.
This method doesn't read data from the bit reader, BUT drops the amount of
bits that correspond to the decoded symbol.
bits MUST contain at least 15 (BROTLI_HUFFMAN_MAX_CODE_LENGTH) valid bits.
*/
func decodeSymbol(bits uint32, table []huffmanCode, br *bitReader) uint32 {
	table = table[bits&huffmanTableMask:]
	if table[0].bits > huffmanTableBits {
		nbits := uint32(table[0].bits) - huffmanTableBits
		br.dropBits(huffmanTableBits)
		table = table[uint32(table[0].value)+((bits>>huffmanTableBits)&bitMask(nbits)):]
	}

	br.dropBits(uint32(table[0].bits))

	return uint32(table[0].value)
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

	table = table[val&huffmanTableMask:]
	if table[0].bits <= huffmanTableBits {
		if uint32(table[0].bits) <= availBits {
			br.dropBits(uint32(table[0].bits))
			*result = uint32(table[0].value)
			return true
		}

		return false /* Not enough bits for the first level. */
	}

	if availBits <= huffmanTableBits {
		return false /* Not enough bits to move to the second level. */
	}

	/* Speculatively drop HUFFMAN_TABLE_BITS. */
	val = (val & bitMask(uint32(table[0].bits))) >> huffmanTableBits
	availBits -= huffmanTableBits

	table = table[uint32(table[0].value)+val:]
	if availBits < uint32(table[0].bits) {
		return false /* Not enough bits for the second level. */
	}

	br.dropBits(huffmanTableBits + uint32(table[0].bits))
	*result = uint32(table[0].value)

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

	table = table[br.getBits(huffmanTableBits):]
	*bits = uint32(table[0].bits)
	*value = uint32(table[0].value)
}

/*
Decodes the next Huffman code using data prepared by PreloadSymbol.
Reads 0 - 15 bits. Also peeks 8 following bits.
*/
func readPreloadedSymbol(table []huffmanCode, br *bitReader, bits, value *uint32) uint32 {
	var (
		result = *value
		ext    []huffmanCode
	)

	if *bits > huffmanTableBits {
		val := br.get16BitsUnmasked()
		ext = table[val&huffmanTableMask:][*value:]

		mask := bitMask(*bits - huffmanTableBits)
		br.dropBits(huffmanTableBits)
		ext = ext[(val>>huffmanTableBits)&mask:]
		br.dropBits(uint32(ext[0].bits))
		result = uint32(ext[0].value)
	} else {
		br.dropBits(*bits)
	}

	preloadSymbol(0, table, br, bits, value)

	return result
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

	switch int(s.substateContextMap) {
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

func (s *Reader) detectTrivialLiteralBlockTypes() {
	for i := uint(0); i < 8; i++ {
		s.trivialLiteralContexts[i] = 0
	}

	for i := uint(0); uint32(i) < s.numBlockTypes[0]; i++ {
		offset := i << literalContextBits

		var errVal uint

		sample := uint(s.contextMap[offset])

		for j := uint(0); j < 1<<literalContextBits; {
			for k := 0; k < 4; k++ {
				errVal |= uint(s.contextMap[offset+j]) ^ sample
				j++
			}
		}

		if errVal == 0 {
			s.trivialLiteralContexts[i>>5] |= 1 << (i & 31)
		}
	}
}

func (s *Reader) prepareLiteralDecoding() {
	blockType := s.blockTypeRB[1]
	contextOffset := blockType << literalContextBits

	s.contextMapSlice = s.contextMap[contextOffset:]
	trivial := uint(s.trivialLiteralContexts[blockType>>5])
	s.trivialLiteralContext = int((trivial >> (blockType & 31)) & 1)
	s.literalHtree = []huffmanCode(s.literalHgroup.htrees[s.contextMapSlice[0]])
	contextMode := s.contextModes[blockType] & 3
	s.contextLookup = getContextLUT(int(contextMode))
}

/*
Decodes the block type and updates the state for literal context.
Reads 3..54 bits.
*/
func (s *Reader) decodeLiteralBlockSwitchInternal(safe int) bool {
	if !s.decodeBlockTypeAndLength(safe, 0) {
		return false
	}

	s.prepareLiteralDecoding()

	return true
}

func (s *Reader) decodeLiteralBlockSwitch() {
	s.decodeLiteralBlockSwitchInternal(0)
}

func (s *Reader) safeDecodeLiteralBlockSwitch() bool {
	return s.decodeLiteralBlockSwitchInternal(1)
}

/*
Block switch for insert/copy length.
Reads 3..54 bits.
*/
func (s *Reader) decodeCommandBlockSwitchInternal(safe int) bool {
	if !s.decodeBlockTypeAndLength(safe, 1) {
		return false
	}

	s.htreeCommand = []huffmanCode(s.insertCopyHgroup.htrees[s.blockTypeRB[3]])

	return true
}

func (s *Reader) decodeCommandBlockSwitch() {
	s.decodeCommandBlockSwitchInternal(0)
}

func (s *Reader) safeDecodeCommandBlockSwitch() bool {
	return s.decodeCommandBlockSwitchInternal(1)
}

/*
Block switch for distance codes.
Reads 3..54 bits.
*/
func (s *Reader) decodeDistanceBlockSwitchInternal(safe int) bool {
	if !s.decodeBlockTypeAndLength(safe, 2) {
		return false
	}

	s.distContextMapSlice = s.distContextMap[s.blockTypeRB[5]<<distanceContextBits:]
	s.distHtreeIndex = s.distContextMapSlice[s.distanceContext]

	return true
}

func (s *Reader) decodeDistanceBlockSwitch() {
	s.decodeDistanceBlockSwitchInternal(0)
}

func (s *Reader) safeDecodeDistanceBlockSwitch() bool {
	return s.decodeDistanceBlockSwitchInternal(1)
}

func (s *Reader) unwrittenBytes(wrap bool) uint {
	var pos uint
	if wrap && s.pos > s.ringbufferSize {
		pos = uint(s.ringbufferSize)
	} else {
		pos = uint(s.pos)
	}

	partialPosRb := (s.rbRoundtrips * uint(s.ringbufferSize)) + pos

	return partialPosRb - s.partialPosOut
}

/*
Dumps output.
Returns BROTLI_DECODER_NEEDS_MORE_OUTPUT only if there is more output to push
and either ring-buffer is as big as window size, or |force| is true.
*/
func (s *Reader) writeRingBuffer(availableOut *uint, nextOut *[]byte, totalOut *uint, force bool) int {
	start := s.ringbuffer[s.partialPosOut&uint(s.ringbufferMask):]

	toWrite := s.unwrittenBytes(true)
	numWritten := *availableOut

	if numWritten > toWrite {
		numWritten = toWrite
	}

	if s.metaBlockRemainingLen < 0 {
		return decoderErrorFormatBlockLength1
	}

	if nextOut != nil && *nextOut == nil {
		*nextOut = start
	} else if nextOut != nil {
		copy(*nextOut, start[:numWritten])
		*nextOut = (*nextOut)[numWritten:]
	}

	*availableOut -= numWritten

	s.partialPosOut += numWritten
	if totalOut != nil {
		*totalOut = s.partialPosOut
	}

	if numWritten < toWrite {
		if s.ringbufferSize == 1<<s.windowBits || force {
			return decoderNeedsMoreOutput
		}

		return decoderSuccess
	}

	/* Wrap ring buffer only if it has reached its maximal size. */
	if s.ringbufferSize == 1<<s.windowBits && s.pos >= s.ringbufferSize {
		s.pos -= s.ringbufferSize

		s.rbRoundtrips++
		if uint(s.pos) != 0 {
			s.shouldWrapRingbuffer = 1
		} else {
			s.shouldWrapRingbuffer = 0
		}
	}

	return decoderSuccess
}

func (s *Reader) wrapRingBuffer() {
	if s.shouldWrapRingbuffer != 0 {
		copy(s.ringbuffer, s.ringbufferEnd[:uint(s.pos)])
		s.shouldWrapRingbuffer = 0
	}
}

/*
Allocates ring-buffer.
s.ringbufferSize MUST be updated by calculateRingBufferSize before this function is called.
Last two bytes of ring-buffer are initialized to 0, so context calculation
could be done uniformly for the first two and all other positions.
*/
func (s *Reader) ensureRingBuffer() bool {
	var oldRingbuffer []byte

	if s.ringbufferSize == s.newRingbufferSize {
		return true
	}

	spaceNeeded := int(s.newRingbufferSize) + int(kRingBufferWriteAheadSlack)
	if len(s.ringbuffer) < spaceNeeded {
		oldRingbuffer = s.ringbuffer
		s.ringbuffer = make([]byte, spaceNeeded)
	}

	s.ringbuffer[s.newRingbufferSize-2] = 0
	s.ringbuffer[s.newRingbufferSize-1] = 0

	if oldRingbuffer != nil {
		copy(s.ringbuffer, oldRingbuffer[:uint(s.pos)])
	}

	s.ringbufferSize = s.newRingbufferSize
	s.ringbufferMask = s.newRingbufferSize - 1
	s.ringbufferEnd = s.ringbuffer[s.ringbufferSize:]

	return true
}

func (s *Reader) copyUncompressedBlockToOutput(availableOut *uint, nextOut *[]byte, totalOut *uint) int {
	if !s.ensureRingBuffer() {
		return decoderErrorAllocRingBuffer1
	}

	for {
		switch s.substateUncompressed {
		case stateUncompressedNone:
			nbytes := int(s.br.remainingBytes())
			if nbytes > s.metaBlockRemainingLen {
				nbytes = s.metaBlockRemainingLen
			}

			if s.pos+nbytes > s.ringbufferSize {
				nbytes = s.ringbufferSize - s.pos
			}

			/* Copy remaining bytes from s.br to ring-buffer. */
			s.br.copyBytes(s.ringbuffer[s.pos:], uint(nbytes))

			s.pos += nbytes
			s.metaBlockRemainingLen -= nbytes

			if s.pos < 1<<s.windowBits {
				if s.metaBlockRemainingLen == 0 {
					return decoderSuccess
				}

				return decoderNeedsMoreInput
			}

			s.substateUncompressed = stateUncompressedWrite

			fallthrough

		case stateUncompressedWrite:
			result := s.writeRingBuffer(availableOut, nextOut, totalOut, false)
			if result != decoderSuccess {
				return result
			}

			if s.ringbufferSize == 1<<s.windowBits {
				s.maxDistance = s.maxBackwardDistance
			}

			s.substateUncompressed = stateUncompressedNone
		}
	}
}

/*
Calculates the smallest feasible ring buffer.
If we know the data size is small, do not allocate more ring buffer
size than needed to reduce memory usage.
When this method is called, metablock size and flags MUST be decoded.
*/
func (s *Reader) calculateRingBufferSize() {
	windowSize := 1 << s.windowBits
	newRingbufferSize := windowSize

	var minSize int

	if s.ringbufferSize != 0 {
		minSize = s.ringbufferSize
	} else {
		minSize = 1024
	}

	/* If maximum is already reached, no further extension is required. */
	if s.ringbufferSize == windowSize {
		return
	}

	/* Metadata blocks do not touch ring buffer. */
	if s.isMetadata != 0 {
		return
	}

	var outputSize int
	if s.ringbuffer != nil {
		outputSize = s.pos
	}

	outputSize += s.metaBlockRemainingLen
	if minSize < outputSize {
		minSize = outputSize
	}

	if s.cannyRingbufferAllocation != 0 {
		for newRingbufferSize>>1 >= minSize {
			newRingbufferSize >>= 1
		}
	}

	s.newRingbufferSize = newRingbufferSize
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

func (s *Reader) takeDistanceFromRingBuffer() {
	if s.distanceCode == 0 {
		s.distRbIdx--
		s.distanceCode = s.distRb[s.distRbIdx&3]

		/* Compensate double distance-ring-buffer roll for dictionary items. */
		s.distanceContext = 1
	} else {
		distanceCode := s.distanceCode << 1

		const (
			kDistanceShortCodeIndexOffset uint32 = 0xAAAFFF1B
			kDistanceShortCodeValueOffset uint32 = 0xFA5FA500
		)

		v := (s.distRbIdx + int(kDistanceShortCodeIndexOffset>>uint(distanceCode))) & 0x3
		s.distanceCode = s.distRb[v]

		v = int(kDistanceShortCodeValueOffset>>uint(distanceCode)) & 0x3
		if distanceCode&0x3 != 0 {
			s.distanceCode += v
		} else {
			s.distanceCode -= v
			if s.distanceCode <= 0 {
				s.distanceCode = 0x7FFFFFFF
			}
		}
	}
}

/* Precondition: s.distanceCode < 0. */
func (s *Reader) readDistanceInternal(safe int) bool {
	var (
		distval      int
		memento      bitReaderState
		distanceTree = []huffmanCode(s.distanceHgroup.htrees[s.distHtreeIndex])
		br           = &s.br
	)

	if safe == 0 {
		s.distanceCode = int(readSymbol(distanceTree, br))
	} else {
		var code uint32

		br.saveState(&memento)

		if !safeReadSymbol(distanceTree, br, &code) {
			return false
		}

		s.distanceCode = int(code)
	}

	/* Convert the distance code to the actual distance by possibly
	   looking up past distances from the s.ringbuffer. */
	s.distanceContext = 0

	if s.distanceCode&^0xF == 0 {
		s.takeDistanceFromRingBuffer()
		s.blockLength[2]--
		return true
	}

	distval = s.distanceCode - int(s.numDirectDistanceCodes)
	if distval >= 0 {
		var (
			nbits   uint32
			postfix int
			offset  int
		)

		if safe == 0 && (s.distancePostfixBits == 0) {
			nbits = (uint32(distval) >> 1) + 1
			offset = ((2 + (distval & 1)) << nbits) - 4
			s.distanceCode = int(s.numDirectDistanceCodes) + offset + int(br.readBits(nbits))
		} else {
			var bits uint32

			postfix = distval & s.distancePostfixMask
			distval >>= s.distancePostfixBits

			nbits = (uint32(distval) >> 1) + 1
			if safe != 0 {
				if !br.safeReadBitsMaybeZero(nbits, &bits) {
					s.distanceCode = -1 /* Restore precondition. */

					br.restoreState(&memento)

					return false
				}
			} else {
				bits = br.readBits(nbits)
			}

			offset = ((2 + (distval & 1)) << nbits) - 4
			s.distanceCode = int(s.numDirectDistanceCodes) + ((offset + int(bits)) << s.distancePostfixBits) + postfix
		}
	}

	s.distanceCode = s.distanceCode - numDistanceShortCodes + 1
	s.blockLength[2]--

	return true
}

func (s *Reader) readDistance() {
	s.readDistanceInternal(0)
}

func (s *Reader) safeReadDistance() bool {
	return s.readDistanceInternal(1)
}

func (s *Reader) readCommandInternal(safe int, insertLength *int) bool {
	var (
		cmdCode        uint32
		insertLenExtra uint32
		copyLength     uint32
		v              cmdLutElement
		memento        bitReaderState
		br             = &s.br
	)

	if safe == 0 {
		cmdCode = readSymbol(s.htreeCommand, br)
	} else {
		br.saveState(&memento)

		if !safeReadSymbol(s.htreeCommand, br, &cmdCode) {
			return false
		}
	}

	v = kCmdLut[cmdCode]
	s.distanceCode = int(v.distanceCode)
	s.distanceContext = int(v.context)
	s.distHtreeIndex = s.distContextMapSlice[s.distanceContext]

	*insertLength = int(v.insertLenOffset)
	if safe == 0 {
		if v.insertLenExtraBits != 0 {
			insertLenExtra = br.readBits(uint32(v.insertLenExtraBits))
		}

		copyLength = br.readBits(uint32(v.copyLenExtraBits))
	} else if !br.safeReadBitsMaybeZero(uint32(v.insertLenExtraBits), &insertLenExtra) ||
		!br.safeReadBitsMaybeZero(uint32(v.copyLenExtraBits), &copyLength) {
		br.restoreState(&memento)
		return false
	}

	s.copyLength = int(copyLength) + int(v.copyLenOffset)
	s.blockLength[1]--
	*insertLength += int(insertLenExtra)

	return true
}

func (s *Reader) readCommand(insertLength *int) {
	s.readCommandInternal(0, insertLength)
}

func (s *Reader) safeReadCommand(insertLength *int) bool {
	return s.readCommandInternal(1, insertLength)
}

func (s *Reader) processCommandsInternal(safe int) int {
	var (
		pos    = s.pos
		i      = s.loopCounter
		result = decoderSuccess
		br     = &s.br
		hc     []huffmanCode
	)

	if safe == 0 && !br.hasInput(28) {
		result = decoderNeedsMoreInput
		goto saveStateAndReturn
	}

	if safe == 0 {
		br.warmup()
	}

	/* Jump into state machine. */
	switch s.state {
	case stateCommandBegin:
		goto CommandBegin
	case stateCommandInner:
		goto CommandInner
	case stateCommandPostDecodeLiterals:
		goto CommandPostDecodeLiterals
	case stateCommandPostWrapCopy:
		goto CommandPostWrapCopy
	default:
		return decoderErrorUnreachable
	}

CommandBegin:
	if safe != 0 {
		s.state = stateCommandBegin
	}

	if safe == 0 && !br.hasInput(28) { /* 156 bits + 7 bytes */
		s.state = stateCommandBegin
		result = decoderNeedsMoreInput
		goto saveStateAndReturn
	}

	if s.blockLength[1] == 0 {
		if safe != 0 {
			if !s.safeDecodeCommandBlockSwitch() {
				result = decoderNeedsMoreInput
				goto saveStateAndReturn
			}
		} else {
			s.decodeCommandBlockSwitch()
		}

		goto CommandBegin
	}

	/* Read the insert/copy length in the command. */
	if safe != 0 {
		if !s.safeReadCommand(&i) {
			result = decoderNeedsMoreInput
			goto saveStateAndReturn
		}
	} else {
		s.readCommand(&i)
	}

	if i == 0 {
		goto CommandPostDecodeLiterals
	}

	s.metaBlockRemainingLen -= i

CommandInner:
	if safe != 0 {
		s.state = stateCommandInner
	}

	/* Read the literals in the command. */
	if s.trivialLiteralContext != 0 {
		var (
			bits  uint32
			value uint32
		)

		preloadSymbol(safe, s.literalHtree, br, &bits, &value)

		for {
			if safe == 0 && !br.hasInput(28) { /* 162 bits + 7 bytes */
				s.state = stateCommandInner
				result = decoderNeedsMoreInput
				goto saveStateAndReturn
			}

			if s.blockLength[0] == 0 {
				if safe != 0 {
					if !s.safeDecodeLiteralBlockSwitch() {
						result = decoderNeedsMoreInput
						goto saveStateAndReturn
					}
				} else {
					s.decodeLiteralBlockSwitch()
				}

				preloadSymbol(safe, s.literalHtree, br, &bits, &value)

				if s.trivialLiteralContext == 0 {
					goto CommandInner
				}
			}

			if safe == 0 {
				s.ringbuffer[pos] = byte(readPreloadedSymbol(s.literalHtree, br, &bits, &value))
			} else {
				var literal uint32
				if !safeReadSymbol(s.literalHtree, br, &literal) {
					result = decoderNeedsMoreInput
					goto saveStateAndReturn
				}

				s.ringbuffer[pos] = byte(literal)
			}

			s.blockLength[0]--

			pos++
			if pos == s.ringbufferSize {
				s.state = stateCommandInnerWrite
				i--
				goto saveStateAndReturn
			}

			i--
			if i == 0 {
				break
			}
		}
	} else {
		p1 := s.ringbuffer[(pos-1)&s.ringbufferMask]
		p2 := s.ringbuffer[(pos-2)&s.ringbufferMask]

		for {
			if safe == 0 && !br.hasInput(28) { /* 162 bits + 7 bytes */
				s.state = stateCommandInner
				result = decoderNeedsMoreInput
				goto saveStateAndReturn
			}

			if s.blockLength[0] == 0 {
				if safe != 0 {
					if !s.safeDecodeLiteralBlockSwitch() {
						result = decoderNeedsMoreInput
						goto saveStateAndReturn
					}
				} else {
					s.decodeLiteralBlockSwitch()
				}

				if s.trivialLiteralContext != 0 {
					goto CommandInner
				}
			}

			context := s.contextLookup.get(p1, p2)
			hc = []huffmanCode(s.literalHgroup.htrees[s.contextMapSlice[context]])

			p2 = p1
			if safe == 0 {
				p1 = byte(readSymbol(hc, br))
			} else {
				var literal uint32
				if !safeReadSymbol(hc, br, &literal) {
					result = decoderNeedsMoreInput
					goto saveStateAndReturn
				}

				p1 = byte(literal)
			}

			s.ringbuffer[pos] = p1
			s.blockLength[0]--

			pos++
			if pos == s.ringbufferSize {
				s.state = stateCommandInnerWrite
				i--
				goto saveStateAndReturn
			}

			i--
			if i == 0 {
				break
			}
		}
	}

	if s.metaBlockRemainingLen <= 0 {
		s.state = stateMetablockDone
		goto saveStateAndReturn
	}

CommandPostDecodeLiterals:
	if safe != 0 {
		s.state = stateCommandPostDecodeLiterals
	}

	if s.distanceCode >= 0 {
		/* Implicit distance case. */
		if s.distanceCode != 0 {
			s.distanceContext = 0
		} else {
			s.distanceContext = 1
		}

		s.distRbIdx--
		s.distanceCode = s.distRb[s.distRbIdx&3]
	} else {
		/* Read distance code in the command, unless it was implicitly zero. */
		if s.blockLength[2] == 0 {
			if safe != 0 {
				if !s.safeDecodeDistanceBlockSwitch() {
					result = decoderNeedsMoreInput
					goto saveStateAndReturn
				}
			} else {
				s.decodeDistanceBlockSwitch()
			}
		}

		if safe != 0 {
			if !s.safeReadDistance() {
				result = decoderNeedsMoreInput
				goto saveStateAndReturn
			}
		} else {
			s.readDistance()
		}
	}

	if s.maxDistance != s.maxBackwardDistance {
		if pos < s.maxBackwardDistance {
			s.maxDistance = pos
		} else {
			s.maxDistance = s.maxBackwardDistance
		}
	}

	i = s.copyLength

	/* Apply copy of LZ77 back-reference, or static dictionary reference if
	   the distance is larger than the max LZ77 distance */
	if s.distanceCode > s.maxDistance {
		if s.distanceCode > maxAllowedDistance {
			return decoderErrorFormatDistance
		}

		if i >= minDictionaryWordLength && i <= maxDictionaryWordLength {
			address := s.distanceCode - s.maxDistance - 1
			words := s.dictionary
			trans := s.transforms
			offset := int(s.dictionary.offsetsByLength[i])
			shift := uint32(s.dictionary.sizeBitsByLength[i])
			mask := int(bitMask(shift))
			wordIdx := address & mask
			transformIdx := address >> shift

			/* Compensate double distance-ring-buffer roll. */
			s.distRbIdx += s.distanceContext

			offset += wordIdx * i

			if words.data == nil {
				return decoderErrorDictionaryNotSet
			}

			if transformIdx < int(trans.numTransforms) {
				word := words.data[offset:]

				wordLen := i
				if transformIdx == int(trans.cutOffTransforms[0]) {
					copy(s.ringbuffer[pos:], word[:uint(wordLen)])
				} else {
					wordLen = trans.transformWord(s.ringbuffer[pos:], word, wordLen, transformIdx)
				}

				pos += wordLen

				s.metaBlockRemainingLen -= wordLen
				if pos >= s.ringbufferSize {
					s.state = stateCommandPostWrite1
					goto saveStateAndReturn
				}
			} else {
				return decoderErrorFormatTransform
			}
		} else {
			return decoderErrorFormatDictionary
		}
	} else {
		srcStart := (pos - s.distanceCode) & s.ringbufferMask

		copyDst := s.ringbuffer[pos:]
		copySrc := s.ringbuffer[srcStart:]

		dstEnd := pos + i
		srcEnd := srcStart + i

		/* Update the recent distances cache. */
		s.distRb[s.distRbIdx&3] = s.distanceCode

		s.distRbIdx++
		s.metaBlockRemainingLen -= i

		copy(copyDst, copySrc[:16])

		if srcEnd > pos && dstEnd > srcStart {
			/* Regions intersect. */
			goto CommandPostWrapCopy
		}

		if dstEnd >= s.ringbufferSize || srcEnd >= s.ringbufferSize {
			/* At least one region wraps. */
			goto CommandPostWrapCopy
		}

		pos += i
		if i > 16 {
			if i > 32 {
				copy(copyDst[16:], copySrc[16:][:uint(i-16)])
			} else {
				copy(copyDst[16:], copySrc[16:][:16])
			}
		}
	}

	if s.metaBlockRemainingLen <= 0 {
		s.state = stateMetablockDone
		goto saveStateAndReturn
	} else {
		goto CommandBegin
	}

CommandPostWrapCopy:
	{
		wrapGuard := s.ringbufferSize - pos
		for {
			i--
			if i < 0 {
				break
			}

			s.ringbuffer[pos] = s.ringbuffer[(pos-s.distanceCode)&s.ringbufferMask]
			pos++

			wrapGuard--
			if wrapGuard == 0 {
				s.state = stateCommandPostWrite2
				goto saveStateAndReturn
			}
		}
	}

	if s.metaBlockRemainingLen <= 0 {
		s.state = stateMetablockDone
		goto saveStateAndReturn
	} else {
		goto CommandBegin
	}

saveStateAndReturn:
	s.pos = pos

	s.loopCounter = i

	return result
}

func (s *Reader) processCommands() int {
	return s.processCommandsInternal(0)
}

func (s *Reader) safeProcessCommands() int {
	return s.processCommandsInternal(1)
}

/* Returns the maximum number of distance symbols which can only represent
   distances not exceeding BROTLI_MAX_ALLOWED_DISTANCE. */

var (
	maxDistanceSymbol_bound = [maxNpostfix + 1]uint32{0, 4, 12, 28}
	maxDistanceSymbol_diff  = [maxNpostfix + 1]uint32{73, 126, 228, 424}
)

func maxDistanceSymbol(ndirect, npostfix uint32) uint32 {
	postfix := uint32(1) << npostfix
	bound := maxDistanceSymbol_bound[npostfix]
	diff := maxDistanceSymbol_diff[npostfix]

	switch {
	case ndirect < bound:
		return ndirect + diff + postfix
	case ndirect > bound+postfix:
		return ndirect + diff
	default:
		return bound + diff + postfix
	}
}

/*
Invariant: input stream is never overconsumed:
  - invalid input implies that the whole stream is invalid -> any amount of
    input could be read and discarded
  - when result is "needs more input", then at least one more byte is REQUIRED
    to complete decoding; all input data MUST be consumed by decoder, so
    client could swap the input buffer
  - when result is "needs more output" decoder MUST ensure that it doesn't
    hold more than 7 bits in bit reader; this saves client from swapping input
    buffer ahead of time
  - when result is "success" decoder MUST return all unused data back to input
    buffer; this is possible because the invariant is held on enter
*/
func (s *Reader) decompressStream(
	availableIn *uint,
	nextIn *[]byte,
	availableOut *uint,
	nextOut *[]byte,
) int {
	var (
		result = decoderSuccess
		br     = &s.br
	)

	/* Do not try to process further in a case of unrecoverable error. */
	if s.errorCode < 0 {
		return decoderResultError
	}

	if *availableOut != 0 && (nextOut == nil || *nextOut == nil) {
		return s.saveErrorCode(decoderErrorInvalidArguments)
	}

	if *availableOut == 0 {
		nextOut = nil
	}

	if s.bufferLength == 0 { /* Just connect bit reader to input stream. */
		br.inputLen = *availableIn
		br.input = *nextIn
		br.bytePos = 0
	} else {
		/* At least one byte of input is required. More than one byte of input may
		   be required to complete the transaction -> reading more data must be
		   done in a loop -> do it in a main loop. */
		result = decoderNeedsMoreInput

		br.input = s.buffer.u8[:]
		br.bytePos = 0
	}

	/* State machine */
	for {
		if result != decoderSuccess {
			/* Error, needs more input/output. */
			if result == decoderNeedsMoreInput {
				if s.ringbuffer != nil { /* Pro-actively push output. */
					intermediateResult := s.writeRingBuffer(availableOut, nextOut, nil, true)

					/* writeRingBuffer checks s.metaBlockRemainingLen validity. */
					if intermediateResult < 0 {
						result = intermediateResult
						break
					}
				}

				if s.bufferLength != 0 { /* Used with internal buffer. */
					if br.bytePos == br.inputLen {
						/* Successfully finished read transaction.
						   Accumulator contains less than 8 bits, because internal buffer
						   is expanded byte-by-byte until it is enough to complete read. */
						s.bufferLength = 0

						/* Switch to input stream and restart. */
						result = decoderSuccess

						br.inputLen = *availableIn
						br.input = *nextIn
						br.bytePos = 0

						continue
					} else if *availableIn != 0 {
						/* Not enough data in buffer, but can take one more byte from input stream. */
						result = decoderSuccess

						s.buffer.u8[s.bufferLength] = (*nextIn)[0]
						s.bufferLength++
						br.inputLen = uint(s.bufferLength)
						*nextIn = (*nextIn)[1:]
						(*availableIn)--

						/* Retry with more data in buffer. */
						continue
					}

					/* Can't finish reading and no more input. */
					break
				} else {
					/* Copy tail to internal buffer and return. */
					*nextIn = br.input[br.bytePos:]

					*availableIn = br.inputLen - br.bytePos
					for *availableIn != 0 {
						s.buffer.u8[s.bufferLength] = (*nextIn)[0]
						s.bufferLength++
						*nextIn = (*nextIn)[1:]
						(*availableIn)--
					}

					break
				}
			}

			/* Fail or needs more output. */
			if s.bufferLength != 0 {
				s.bufferLength = 0
			} else {
				br.unload()

				*availableIn = br.inputLen - br.bytePos
				*nextIn = br.input[br.bytePos:]
			}

			break
		}

		switch s.state {
		/* Prepare to the first read. */
		case stateUninited:
			if !br.warmup() {
				result = decoderNeedsMoreInput
				break
			}

			/* Decode window size. */
			result = s.decodeWindowBits()
			if result != decoderSuccess {
				break
			}

			if s.largeWindow {
				s.state = stateLargeWindowBits
				break
			}

			s.state = stateInitialize

		case stateLargeWindowBits:
			if !br.safeReadBits(6, &s.windowBits) {
				result = decoderNeedsMoreInput
				break
			}

			if s.windowBits < largeMinWbits || s.windowBits > largeMaxWbits {
				result = decoderErrorFormatWindowBits
				break
			}

			s.state = stateInitialize

			fallthrough

		/* Fall through. */
		case stateInitialize:
			s.maxBackwardDistance = (1 << s.windowBits) - windowGap

			if s.blockTypeTrees == nil {
				/* Allocate memory for both block_type_trees and block_len_trees. */
				s.blockTypeTrees = make([]huffmanCode, (3 * (huffmanMaxSize258 + huffmanMaxSize26)))
			}

			s.blockLenTrees = s.blockTypeTrees[3*huffmanMaxSize258:]

			s.state = stateMetablockBegin

			fallthrough

		case stateMetablockBegin:
			s.metablockBegin()

			s.state = stateMetablockHeader

			fallthrough

		case stateMetablockHeader:
			result = s.decodeMetaBlockLength()
			/* Reads 2 - 31 bits. */
			if result != decoderSuccess {
				break
			}

			if s.isMetadata != 0 || s.isUncompressed != 0 {
				if !br.jumpToByteBoundary() {
					result = decoderErrorFormatPadding1
					break
				}
			}

			if s.isMetadata != 0 {
				s.state = stateMetadata
				break
			}

			if s.metaBlockRemainingLen == 0 {
				s.state = stateMetablockDone
				break
			}

			s.calculateRingBufferSize()

			if s.isUncompressed != 0 {
				s.state = stateUncompressed
				break
			}

			s.loopCounter = 0
			s.state = stateHuffmanCode0

		case stateUncompressed:
			result = s.copyUncompressedBlockToOutput(availableOut, nextOut, nil)
			if result == decoderSuccess {
				s.state = stateMetablockDone
			}

		case stateMetadata:
			for ; s.metaBlockRemainingLen > 0; s.metaBlockRemainingLen-- {
				var bits uint32

				/* Read one byte and ignore it. */
				if !br.safeReadBits(8, &bits) {
					result = decoderNeedsMoreInput
					break
				}
			}

			if result == decoderSuccess {
				s.state = stateMetablockDone
			}

		case stateHuffmanCode0:
			if s.loopCounter >= 3 {
				s.state = stateMetablockHeader2
				break
			}

			/* Reads 1..11 bits. */
			result = s.decodeVarLenUint8(&s.numBlockTypes[s.loopCounter])

			if result != decoderSuccess {
				break
			}

			s.numBlockTypes[s.loopCounter]++
			if s.numBlockTypes[s.loopCounter] < 2 {
				s.loopCounter++
				break
			}

			s.state = stateHuffmanCode1

			fallthrough

		case stateHuffmanCode1:
			{
				alphabetSize := s.numBlockTypes[s.loopCounter] + 2
				treeOffset := s.loopCounter * huffmanMaxSize258

				result = s.readHuffmanCode(alphabetSize, alphabetSize, s.blockTypeTrees[treeOffset:], nil)
				if result != decoderSuccess {
					break
				}

				s.state = stateHuffmanCode2
			}

			fallthrough

		case stateHuffmanCode2:
			{
				alphabetSize := uint32(numBlockLenSymbols)
				treeOffset := s.loopCounter * huffmanMaxSize26

				result = s.readHuffmanCode(alphabetSize, alphabetSize, s.blockLenTrees[treeOffset:], nil)
				if result != decoderSuccess {
					break
				}

				s.state = stateHuffmanCode3
			}

			fallthrough

		case stateHuffmanCode3:
			treeOffset := s.loopCounter * huffmanMaxSize26
			if !s.safeReadBlockLength(&s.blockLength[s.loopCounter], s.blockLenTrees[treeOffset:]) {
				result = decoderNeedsMoreInput
				break
			}

			s.loopCounter++
			s.state = stateHuffmanCode0

		case stateMetablockHeader2:
			{
				var bits uint32
				if !br.safeReadBits(6, &bits) {
					result = decoderNeedsMoreInput
					break
				}

				s.distancePostfixBits = bits & bitMask(2)
				bits >>= 2
				s.numDirectDistanceCodes = numDistanceShortCodes + (bits << s.distancePostfixBits)
				s.distancePostfixMask = int(bitMask(s.distancePostfixBits))

				s.contextModes = make([]byte, uint(s.numBlockTypes[0]))
				if s.contextModes == nil {
					result = decoderErrorAllocContextModes
					break
				}

				s.loopCounter = 0
				s.state = stateContextModes
			}

			fallthrough

		case stateContextModes:
			result = s.readContextModes()

			if result != decoderSuccess {
				break
			}

			s.state = stateContextMap1

			fallthrough

		case stateContextMap1:
			result = s.decodeContextMap(s.numBlockTypes[0]<<literalContextBits, &s.numLiteralHtrees, &s.contextMap)

			if result != decoderSuccess {
				break
			}

			s.detectTrivialLiteralBlockTypes()
			s.state = stateContextMap2

			fallthrough

		case stateContextMap2:
			{
				numDirectCodes := s.numDirectDistanceCodes - numDistanceShortCodes

				var (
					numDistanceCodes uint32
					maxDistSymbol    uint32
				)

				if s.largeWindow {
					numDistanceCodes = distanceAlphabetSize(
						s.distancePostfixBits,
						numDirectCodes,
						largeMaxDistanceBits,
					)
					maxDistSymbol = maxDistanceSymbol(numDirectCodes, s.distancePostfixBits)
				} else {
					numDistanceCodes = distanceAlphabetSize(s.distancePostfixBits, numDirectCodes, maxDistanceBits)
					maxDistSymbol = numDistanceCodes
				}

				result = s.decodeContextMap(
					s.numBlockTypes[2]<<distanceContextBits,
					&s.numDistHtrees,
					&s.distContextMap,
				)
				if result != decoderSuccess {
					break
				}

				s.literalHgroup.init(numLiteralSymbols, numLiteralSymbols, s.numLiteralHtrees)
				s.insertCopyHgroup.init(numCommandSymbols, numCommandSymbols, s.numBlockTypes[1])
				s.distanceHgroup.init(numDistanceCodes, maxDistSymbol, s.numDistHtrees)

				s.loopCounter = 0
				s.state = stateTreeGroup
			}

			fallthrough

		case stateTreeGroup:
			var hgroup *huffmanTreeGroup
			switch s.loopCounter {
			case 0:
				hgroup = &s.literalHgroup
			case 1:
				hgroup = &s.insertCopyHgroup
			case 2:
				hgroup = &s.distanceHgroup
			default:
				return s.saveErrorCode(decoderErrorUnreachable)
			}

			result = s.decodeHuffmanTreeGroup(hgroup)
			if result != decoderSuccess {
				break
			}

			s.loopCounter++
			if s.loopCounter >= 3 {
				s.prepareLiteralDecoding()
				s.distContextMapSlice = s.distContextMap

				s.htreeCommand = []huffmanCode(s.insertCopyHgroup.htrees[0])
				if !s.ensureRingBuffer() {
					result = decoderErrorAllocRingBuffer2
					break
				}

				s.state = stateCommandBegin
			}

		case stateCommandBegin, stateCommandInner, stateCommandPostDecodeLiterals, stateCommandPostWrapCopy:
			result = s.processCommands()

			if result == decoderNeedsMoreInput {
				result = s.safeProcessCommands()
			}

		case stateCommandInnerWrite, stateCommandPostWrite1, stateCommandPostWrite2:
			result = s.writeRingBuffer(availableOut, nextOut, nil, false)

			if result != decoderSuccess {
				break
			}

			s.wrapRingBuffer()

			if s.ringbufferSize == 1<<s.windowBits {
				s.maxDistance = s.maxBackwardDistance
			}

			switch s.state {
			case stateCommandPostWrite1:
				if s.metaBlockRemainingLen == 0 {
					/* Next metablock, if any. */
					s.state = stateMetablockDone
				} else {
					s.state = stateCommandBegin
				}

			case stateCommandPostWrite2:
				s.state = stateCommandPostWrapCopy /* BROTLI_STATE_COMMAND_INNER_WRITE */
			default:
				if s.loopCounter == 0 {
					if s.metaBlockRemainingLen == 0 {
						s.state = stateMetablockDone
					} else {
						s.state = stateCommandPostDecodeLiterals
					}

					break
				}

				s.state = stateCommandInner
			}

		case stateMetablockDone:
			if s.metaBlockRemainingLen < 0 {
				result = decoderErrorFormatBlockLength2
				break
			}

			s.cleanupAfterMetablock()

			if s.isLastMetablock == 0 {
				s.state = stateMetablockBegin
				break
			}

			if !br.jumpToByteBoundary() {
				result = decoderErrorFormatPadding2
				break
			}

			if s.bufferLength == 0 {
				br.unload()
				*availableIn = br.inputLen - br.bytePos
				*nextIn = br.input[br.bytePos:]
			}

			s.state = stateDone

			fallthrough

		case stateDone:
			if s.ringbuffer != nil {
				result = s.writeRingBuffer(availableOut, nextOut, nil, true)
				if result != decoderSuccess {
					break
				}
			}

			return s.saveErrorCode(result)
		}
	}

	return s.saveErrorCode(result)
}

func (s *Reader) hasMoreOutput() bool {
	/* After unrecoverable error remaining output is considered nonsensical. */
	if int(s.errorCode) < 0 {
		return false
	}

	return s.ringbuffer != nil && s.unwrittenBytes(false) != 0
}

func (s *Reader) getErrorCode() int {
	return int(s.errorCode)
}

func decoderErrorString(c int) string {
	switch c {
	case decoderNoError:
		return "NO_ERROR"
	case decoderSuccess:
		return "SUCCESS"
	case decoderNeedsMoreInput:
		return "NEEDS_MORE_INPUT"
	case decoderNeedsMoreOutput:
		return "NEEDS_MORE_OUTPUT"
	case decoderErrorFormatExuberantNibble:
		return "EXUBERANT_NIBBLE"
	case decoderErrorFormatReserved:
		return "RESERVED"
	case decoderErrorFormatExuberantMetaNibble:
		return "EXUBERANT_META_NIBBLE"
	case decoderErrorFormatSimpleHuffmanAlphabet:
		return "SIMPLE_HUFFMAN_ALPHABET"
	case decoderErrorFormatSimpleHuffmanSame:
		return "SIMPLE_HUFFMAN_SAME"
	case decoderErrorFormatClSpace:
		return "CL_SPACE"
	case decoderErrorFormatHuffmanSpace:
		return "HUFFMAN_SPACE"
	case decoderErrorFormatContextMapRepeat:
		return "CONTEXT_MAP_REPEAT"
	case decoderErrorFormatBlockLength1:
		return "BLOCK_LENGTH_1"
	case decoderErrorFormatBlockLength2:
		return "BLOCK_LENGTH_2"
	case decoderErrorFormatTransform:
		return "TRANSFORM"
	case decoderErrorFormatDictionary:
		return "DICTIONARY"
	case decoderErrorFormatWindowBits:
		return "WINDOW_BITS"
	case decoderErrorFormatPadding1:
		return "PADDING_1"
	case decoderErrorFormatPadding2:
		return "PADDING_2"
	case decoderErrorFormatDistance:
		return "DISTANCE"
	case decoderErrorDictionaryNotSet:
		return "DICTIONARY_NOT_SET"
	case decoderErrorInvalidArguments:
		return "INVALID_ARGUMENTS"
	case decoderErrorAllocContextModes:
		return "CONTEXT_MODES"
	case decoderErrorAllocTreeGroups:
		return "TREE_GROUPS"
	case decoderErrorAllocContextMap:
		return "CONTEXT_MAP"
	case decoderErrorAllocRingBuffer1:
		return "RING_BUFFER_1"
	case decoderErrorAllocRingBuffer2:
		return "RING_BUFFER_2"
	case decoderErrorAllocBlockTypeTrees:
		return "BLOCK_TYPE_TREES"
	case decoderErrorUnreachable:
		return "UNREACHABLE"
	default:
		return "INVALID"
	}
}
