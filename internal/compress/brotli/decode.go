package brotli

/* Copyright 2013 Google Inc. All Rights Reserved.

   Distributed under MIT license.
   See file LICENSE for detail or copy at https://opensource.org/licenses/MIT
*/

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
func saveErrorCode(s *Reader, e int) int {
	s.errorCode = int(e)
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
func decodeWindowBits(s *Reader, br *bitReader) int {
	var (
		n            uint32
		large_window = s.largeWindow
	)

	s.largeWindow = false

	takeBits(br, 1, &n)

	if n == 0 {
		s.windowBits = 16
		return decoderSuccess
	}

	takeBits(br, 3, &n)

	if n != 0 {
		s.windowBits = 17 + n
		return decoderSuccess
	}

	takeBits(br, 3, &n)

	if n == 1 {
		if large_window {
			takeBits(br, 1, &n)

			if n == 1 {
				return decoderErrorFormatWindowBits
			}

			s.largeWindow = true

			return decoderSuccess
		} else {
			return decoderErrorFormatWindowBits
		}
	}

	if n != 0 {
		s.windowBits = 8 + n
		return decoderSuccess
	}

	s.windowBits = 17

	return decoderSuccess
}

/* Decodes a number in the range [0..255], by reading 1 - 11 bits. */
func decodeVarLenUint8(s *Reader, br *bitReader, value *uint32) int {
	var bits uint32
	switch s.substateDecodeUint8 {
	case stateDecodeUint8None:
		if !safeReadBits(br, 1, &bits) {
			return decoderNeedsMoreInput
		}

		if bits == 0 {
			*value = 0
			return decoderSuccess
		}

		fallthrough

		/* Fall through. */

	case stateDecodeUint8Short:
		if !safeReadBits(br, 3, &bits) {
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

		/* Fall through. */

	case stateDecodeUint8Long:
		if !safeReadBits(br, *value, &bits) {
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
func decodeMetaBlockLength(s *Reader, br *bitReader) int {
	var (
		bits uint32
		i    int
	)

	for {
		switch s.substateMetablockHeader {
		case stateMetablockHeaderNone:
			if !safeReadBits(br, 1, &bits) {
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

			/* Fall through. */

		case stateMetablockHeaderEmpty:
			if !safeReadBits(br, 1, &bits) {
				return decoderNeedsMoreInput
			}

			if bits != 0 {
				s.substateMetablockHeader = stateMetablockHeaderNone
				return decoderSuccess
			}

			s.substateMetablockHeader = stateMetablockHeaderNibbles

			fallthrough

			/* Fall through. */

		case stateMetablockHeaderNibbles:
			if !safeReadBits(br, 2, &bits) {
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

			/* Fall through. */

		case stateMetablockHeaderSize:
			i = s.loopCounter

			for ; i < int(s.sizeNibbles); i++ {
				if !safeReadBits(br, 4, &bits) {
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

			/* Fall through. */

		case stateMetablockHeaderUncompressed:
			if s.isLastMetablock == 0 {
				if !safeReadBits(br, 1, &bits) {
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
			if !safeReadBits(br, 1, &bits) {
				return decoderNeedsMoreInput
			}

			if bits != 0 {
				return decoderErrorFormatReserved
			}

			s.substateMetablockHeader = stateMetablockHeaderBytes

			fallthrough

			/* Fall through. */

		case stateMetablockHeaderBytes:
			if !safeReadBits(br, 2, &bits) {
				return decoderNeedsMoreInput
			}

			if bits == 0 {
				s.substateMetablockHeader = stateMetablockHeaderNone
				return decoderSuccess
			}

			s.sizeNibbles = uint(byte(bits))
			s.substateMetablockHeader = stateMetablockHeaderMetadata

			fallthrough

			/* Fall through. */

		case stateMetablockHeaderMetadata:
			i = s.loopCounter

			for ; i < int(s.sizeNibbles); i++ {
				if !safeReadBits(br, 8, &bits) {
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
		dropBits(br, huffmanTableBits)
		table = table[uint32(table[0].value)+((bits>>huffmanTableBits)&bitMask(nbits)):]
	}

	dropBits(br, uint32(table[0].bits))

	return uint32(table[0].value)
}

/*
Reads and decodes the next Huffman code from bit-stream.

	This method peeks 16 bits of input and drops 0 - 15 of them.
*/
func readSymbol(table []huffmanCode, br *bitReader) uint32 {
	return decodeSymbol(get16BitsUnmasked(br), table, br)
}

/*
Same as DecodeSymbol, but it is known that there is less than 15 bits of

	input are currently available.
*/
func safeDecodeSymbol(table []huffmanCode, br *bitReader, result *uint32) bool {
	var (
		val            uint32
		available_bits = getAvailableBits(br)
	)

	if available_bits == 0 {
		if table[0].bits == 0 {
			*result = uint32(table[0].value)
			return true
		}

		return false /* No valid bits at all. */
	}

	val = uint32(getBitsUnmasked(br))

	table = table[val&huffmanTableMask:]
	if table[0].bits <= huffmanTableBits {
		if uint32(table[0].bits) <= available_bits {
			dropBits(br, uint32(table[0].bits))
			*result = uint32(table[0].value)
			return true
		} else {
			return false /* Not enough bits for the first level. */
		}
	}

	if available_bits <= huffmanTableBits {
		return false /* Not enough bits to move to the second level. */
	}

	/* Speculatively drop HUFFMAN_TABLE_BITS. */
	val = (val & bitMask(uint32(table[0].bits))) >> huffmanTableBits

	available_bits -= huffmanTableBits

	table = table[uint32(table[0].value)+val:]
	if available_bits < uint32(table[0].bits) {
		return false /* Not enough bits for the second level. */
	}

	dropBits(br, huffmanTableBits+uint32(table[0].bits))
	*result = uint32(table[0].value)

	return true
}

func safeReadSymbol(table []huffmanCode, br *bitReader, result *uint32) bool {
	var val uint32
	if safeGetBits(br, 15, &val) {
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

	table = table[getBits(br, huffmanTableBits):]
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
		val := get16BitsUnmasked(br)

		ext = table[val&huffmanTableMask:][*value:]

		mask := bitMask((*bits - huffmanTableBits))
		dropBits(br, huffmanTableBits)
		ext = ext[(val>>huffmanTableBits)&mask:]
		dropBits(br, uint32(ext[0].bits))
		result = uint32(ext[0].value)
	} else {
		dropBits(br, *bits)
	}

	preloadSymbol(0, table, br, bits, value)

	return result
}

func log2Floor(x uint32) uint32 {
	var result uint32 = 0
	for x != 0 {
		x >>= 1
		result++
	}

	return result
}

/*
Reads (s->symbol + 1) symbols.

	Totally 1..4 symbols are read, 1..11 bits each.
	The list of symbols MUST NOT contain duplicates.
*/
func readSimpleHuffmanSymbols(alphabet_size, max_symbol uint32, s *Reader) int {
	var (
		br       = &s.br
		max_bits = log2Floor(alphabet_size - 1)
		i        = s.subLoopCounter
	)
	/* max_bits == 1..11; symbol == 0..3; 1..44 bits will be read. */

	num_symbols := s.symbol
	for i <= num_symbols {
		var v uint32
		if !safeReadBits(br, max_bits, &v) {
			s.subLoopCounter = i
			s.substateHuffman = stateHuffmanSimpleRead
			return decoderNeedsMoreInput
		}

		if v >= max_symbol {
			return decoderErrorFormatSimpleHuffmanAlphabet
		}

		s.symbolsListsArray[i] = uint16(v)
		i++
	}

	for i = 0; i < num_symbols; i++ {
		k := i + 1
		for ; k <= num_symbols; k++ {
			if s.symbolsListsArray[i] == s.symbolsListsArray[k] {
				return decoderErrorFormatSimpleHuffmanSame
			}
		}
	}

	return decoderSuccess
}

/*
Process single decoded symbol code length:

	A) reset the repeat variable
	B) remember code length (if it is not 0)
	C) extend corresponding index-chain
	D) reduce the Huffman space
	E) update the histogram
*/
func processSingleCodeLength(
	code_len uint32,
	symbol, repeat, space, prev_code_len *uint32,
	symbol_lists symbolList,
	code_length_histo []uint16,
	next_symbol []int,
) {
	*repeat = 0

	if code_len != 0 { /* code_len == 1..15 */
		symbolListPut(symbol_lists, next_symbol[code_len], uint16(*symbol))
		next_symbol[code_len] = int(*symbol)
		*prev_code_len = code_len
		*space -= 32768 >> code_len
		code_length_histo[code_len]++
	}

	(*symbol)++
}

/*
Process repeated symbol code length.

	 A) Check if it is the extension of previous repeat sequence; if the decoded
	    value is not BROTLI_REPEAT_PREVIOUS_CODE_LENGTH, then it is a new
	    symbol-skip
	 B) Update repeat variable
	 C) Check if operation is feasible (fits alphabet)
	 D) For each symbol do the same operations as in ProcessSingleCodeLength

	PRECONDITION: code_len == BROTLI_REPEAT_PREVIOUS_CODE_LENGTH or
	              code_len == BROTLI_REPEAT_ZERO_CODE_LENGTH
*/
func processRepeatedCodeLength(
	code_len, repeat_delta, alphabet_size uint32,
	symbol, repeat, space, prev_code_len, repeat_code_len *uint32,
	symbol_lists symbolList,
	code_length_histo []uint16,
	next_symbol []int,
) {
	var old_repeat uint32 /* for BROTLI_REPEAT_ZERO_CODE_LENGTH */ /* for BROTLI_REPEAT_ZERO_CODE_LENGTH */

	var (
		extra_bits uint32 = 3
		new_len    uint32 = 0
	)

	if code_len == repeatPreviousCodeLength {
		new_len = *prev_code_len
		extra_bits = 2
	}

	if *repeat_code_len != new_len {
		*repeat = 0
		*repeat_code_len = new_len
	}

	old_repeat = *repeat
	if *repeat > 0 {
		*repeat -= 2
		*repeat <<= extra_bits
	}

	*repeat += repeat_delta + 3

	repeat_delta = *repeat - old_repeat
	if *symbol+repeat_delta > alphabet_size {
		*symbol = alphabet_size
		*space = 0xFFFFF
		return
	}

	if *repeat_code_len != 0 {
		var (
			last = uint(*symbol + repeat_delta)
			next = next_symbol[*repeat_code_len]
		)

		for {
			symbolListPut(symbol_lists, next, uint16(*symbol))
			next = int(*symbol)

			(*symbol)++
			if (*symbol) == uint32(last) {
				break
			}
		}

		next_symbol[*repeat_code_len] = next
		*space -= repeat_delta << (15 - *repeat_code_len)
		code_length_histo[*repeat_code_len] = uint16(uint32(code_length_histo[*repeat_code_len]) + repeat_delta)
	} else {
		*symbol += repeat_delta
	}
}

/* Reads and decodes symbol codelengths. */
func readSymbolCodeLengths(alphabet_size uint32, s *Reader) int {
	var (
		br                = &s.br
		symbol            = s.symbol
		repeat            = s.repeat
		space             = s.space
		prev_code_len     = s.prevCodeLen
		repeat_code_len   = s.repeatCodeLen
		symbol_lists      = s.symbolLists
		code_length_histo = s.codeLengthHisto[:]
		next_symbol       = s.nextSymbol[:]
	)

	if !warmupBitReader(br) {
		return decoderNeedsMoreInput
	}

	var p []huffmanCode
	for symbol < alphabet_size && space > 0 {
		p = s.table[:]

		var code_len uint32

		if !checkInputAmount(br, shortFillBitWindowRead) {
			s.symbol = symbol
			s.repeat = repeat
			s.prevCodeLen = prev_code_len
			s.repeatCodeLen = repeat_code_len
			s.space = space

			return decoderNeedsMoreInput
		}

		fillBitWindow16(br)
		p = p[getBitsUnmasked(br)&uint64(bitMask(huffmanMaxCodeLengthCodeLength)):]
		dropBits(br, uint32(p[0].bits)) /* Use 1..5 bits. */

		code_len = uint32(p[0].value) /* code_len == 0..17 */
		if code_len < repeatPreviousCodeLength {
			processSingleCodeLength(
				code_len,
				&symbol,
				&repeat,
				&space,
				&prev_code_len,
				symbol_lists,
				code_length_histo,
				next_symbol,
			) /* code_len == 16..17, extra_bits == 2..3 */
		} else {
			var extra_bits uint32
			if code_len == repeatPreviousCodeLength {
				extra_bits = 2
			} else {
				extra_bits = 3
			}

			repeat_delta := uint32(getBitsUnmasked(br)) & bitMask(extra_bits)
			dropBits(br, extra_bits)
			processRepeatedCodeLength(
				code_len,
				repeat_delta,
				alphabet_size,
				&symbol,
				&repeat,
				&space,
				&prev_code_len,
				&repeat_code_len,
				symbol_lists,
				code_length_histo,
				next_symbol,
			)
		}
	}

	s.space = space

	return decoderSuccess
}

func safeReadSymbolCodeLengths(alphabet_size uint32, s *Reader) int {
	var (
		br       = &s.br
		get_byte = false
		p        []huffmanCode
	)

	for s.symbol < alphabet_size && s.space > 0 {
		p = s.table[:]

		var (
			code_len       uint32
			available_bits uint32
			bits           uint32 = 0
		)

		if get_byte && !pullByte(br) {
			return decoderNeedsMoreInput
		}

		get_byte = false

		available_bits = getAvailableBits(br)
		if available_bits != 0 {
			bits = uint32(getBitsUnmasked(br))
		}

		p = p[bits&bitMask(huffmanMaxCodeLengthCodeLength):]
		if uint32(p[0].bits) > available_bits {
			get_byte = true
			continue
		}

		code_len = uint32(p[0].value) /* code_len == 0..17 */
		if code_len < repeatPreviousCodeLength {
			dropBits(br, uint32(p[0].bits))
			processSingleCodeLength(
				code_len,
				&s.symbol,
				&s.repeat,
				&s.space,
				&s.prevCodeLen,
				s.symbolLists,
				s.codeLengthHisto[:],
				s.nextSymbol[:],
			) /* code_len == 16..17, extra_bits == 2..3 */
		} else {
			var (
				extra_bits   = code_len - 14
				repeat_delta = (bits >> p[0].bits) & bitMask(extra_bits)
			)

			if available_bits < uint32(p[0].bits)+extra_bits {
				get_byte = true
				continue
			}

			dropBits(br, uint32(p[0].bits)+extra_bits)
			processRepeatedCodeLength(
				code_len,
				repeat_delta,
				alphabet_size,
				&s.symbol,
				&s.repeat,
				&s.space,
				&s.prevCodeLen,
				&s.repeatCodeLen,
				s.symbolLists,
				s.codeLengthHisto[:],
				s.nextSymbol[:],
			)
		}
	}

	return decoderSuccess
}

/*
Reads and decodes 15..18 codes using static prefix code.

	Each code is 2..4 bits long. In total 30..72 bits are used.
*/
func readCodeLengthCodeLengths(s *Reader) int {
	var (
		br        = &s.br
		num_codes = s.repeat
		space     = s.space
		i         = s.subLoopCounter
	)

	for ; i < codeLengthCodes; i++ {
		var (
			code_len_idx = kCodeLengthCodeOrder[i]
			ix           uint32
			v            uint32
		)

		if !safeGetBits(br, 4, &ix) {
			available_bits := getAvailableBits(br)
			if available_bits != 0 {
				ix = uint32(getBitsUnmasked(br) & 0xF)
			} else {
				ix = 0
			}

			if uint32(kCodeLengthPrefixLength[ix]) > available_bits {
				s.subLoopCounter = i
				s.repeat = num_codes
				s.space = space
				s.substateHuffman = stateHuffmanComplex

				return decoderNeedsMoreInput
			}
		}

		v = uint32(kCodeLengthPrefixValue[ix])
		dropBits(br, uint32(kCodeLengthPrefixLength[ix]))

		s.codeLengthCodeLengths[code_len_idx] = byte(v)
		if v != 0 {
			space -= 32 >> v
			num_codes++
			s.codeLengthHisto[v]++

			if space-1 >= 32 {
				/* space is 0 or wrapped around. */
				break
			}
		}
	}

	if num_codes != 1 && space != 0 {
		return decoderErrorFormatClSpace
	}

	return decoderSuccess
}

/*
Decodes the Huffman tables.

	There are 2 scenarios:
	 A) Huffman code contains only few symbols (1..4). Those symbols are read
	    directly; their code lengths are defined by the number of symbols.
	    For this scenario 4 - 49 bits will be read.

	 B) 2-phase decoding:
	 B.1) Small Huffman table is decoded; it is specified with code lengths
	      encoded with predefined entropy code. 32 - 74 bits are used.
	 B.2) Decoded table is used to decode code lengths of symbols in resulting
	      Huffman table. In worst case 3520 bits are read.
*/
func readHuffmanCode(alphabet_size, max_symbol uint32, table []huffmanCode, opt_table_size *uint32, s *Reader) int {
	br := &s.br

	/* Unnecessary masking, but might be good for safety. */
	alphabet_size &= 0x7FF

	/* State machine. */
	for {
		switch s.substateHuffman {
		case stateHuffmanNone:
			if !safeReadBits(br, 2, &s.subLoopCounter) {
				return decoderNeedsMoreInput
			}

			/* The value is used as follows:
			   1 for simple code;
			   0 for no skipping, 2 skips 2 code lengths, 3 skips 3 code lengths */
			if s.subLoopCounter != 1 {
				s.space = 32
				s.repeat = 0 /* num_codes */

				var i int
				for i = 0; i <= huffmanMaxCodeLengthCodeLength; i++ {
					s.codeLengthHisto[i] = 0
				}

				for i = 0; i < codeLengthCodes; i++ {
					s.codeLengthCodeLengths[i] = 0
				}

				s.substateHuffman = stateHuffmanComplex

				continue
			}

			fallthrough

			/* Read symbols, codes & code lengths directly. */

		case stateHuffmanSimpleSize:
			if !safeReadBits(br, 2, &s.symbol) { /* num_symbols */
				s.substateHuffman = stateHuffmanSimpleSize
				return decoderNeedsMoreInput
			}

			s.subLoopCounter = 0

			fallthrough

		case stateHuffmanSimpleRead:
			{
				result := readSimpleHuffmanSymbols(alphabet_size, max_symbol, s)
				if result != decoderSuccess {
					return result
				}
			}

			fallthrough

		case stateHuffmanSimpleBuild:
			var table_size uint32

			if s.symbol == 3 {
				var bits uint32
				if !safeReadBits(br, 1, &bits) {
					s.substateHuffman = stateHuffmanSimpleBuild
					return decoderNeedsMoreInput
				}

				s.symbol += bits
			}

			table_size = buildSimpleHuffmanTable(table, huffmanTableBits, s.symbolsListsArray[:], s.symbol)
			if opt_table_size != nil {
				*opt_table_size = table_size
			}

			s.substateHuffman = stateHuffmanNone

			return decoderSuccess

			/* Decode Huffman-coded code lengths. */

		case stateHuffmanComplex:
			{
				var (
					i      uint32
					result = readCodeLengthCodeLengths(s)
				)

				if result != decoderSuccess {
					return result
				}

				buildCodeLengthsHuffmanTable(s.table[:], s.codeLengthCodeLengths[:], s.codeLengthHisto[:])

				for i = 0; i < 16; i++ {
					s.codeLengthHisto[i] = 0
				}

				for i = 0; i <= huffmanMaxCodeLength; i++ {
					s.nextSymbol[i] = int(i) - (huffmanMaxCodeLength + 1)
					symbolListPut(s.symbolLists, s.nextSymbol[i], 0xFFFF)
				}

				s.symbol = 0
				s.prevCodeLen = initialRepeatedCodeLength
				s.repeat = 0
				s.repeatCodeLen = 0
				s.space = 32768
				s.substateHuffman = stateHuffmanLengthSymbols
			}

			fallthrough

		case stateHuffmanLengthSymbols:
			var (
				table_size uint32
				result     = readSymbolCodeLengths(max_symbol, s)
			)

			if result == decoderNeedsMoreInput {
				result = safeReadSymbolCodeLengths(max_symbol, s)
			}

			if result != decoderSuccess {
				return result
			}

			if s.space != 0 {
				return decoderErrorFormatHuffmanSpace
			}

			table_size = buildHuffmanTable(table, huffmanTableBits, s.symbolLists, s.codeLengthHisto[:])
			if opt_table_size != nil {
				*opt_table_size = table_size
			}

			s.substateHuffman = stateHuffmanNone

			return decoderSuccess

		default:
			return decoderErrorUnreachable
		}
	}
}

/* Decodes a block length by reading 3..39 bits. */
func readBlockLength(table []huffmanCode, br *bitReader) uint32 {
	var (
		code  uint32
		nbits uint32
	)

	code = readSymbol(table, br)
	nbits = kBlockLengthPrefixCode[code].nbits /* nbits == 2..24 */

	return kBlockLengthPrefixCode[code].offset + readBits(br, nbits)
}

/*
WARNING: if state is not BROTLI_STATE_READ_BLOCK_LENGTH_NONE, then

	reading can't be continued with ReadBlockLength.
*/
func safeReadBlockLength(s *Reader, result *uint32, table []huffmanCode, br *bitReader) bool {
	var index uint32
	if s.substateReadBlockLength == stateReadBlockLengthNone {
		if !safeReadSymbol(table, br, &index) {
			return false
		}
	} else {
		index = s.blockLengthIndex
	}

	{
		var bits uint32 /* nbits == 2..24 */

		nbits := kBlockLengthPrefixCode[index].nbits
		if !safeReadBits(br, nbits, &bits) {
			s.blockLengthIndex = index
			s.substateReadBlockLength = stateReadBlockLengthSuffix
			return false
		}

		*result = kBlockLengthPrefixCode[index].offset + bits
		s.substateReadBlockLength = stateReadBlockLengthNone

		return true
	}
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
func inverseMoveToFrontTransform(v []byte, v_len uint32, state *Reader) {
	var (
		mtf [256]byte
		i   int
	)

	for i = 1; i < 256; i++ {
		mtf[i] = byte(i)
	}

	var mtf_1 byte

	/* Transform the input. */
	for i = 0; uint32(i) < v_len; i++ {
		var (
			index = int(v[i])
			value = mtf[index]
		)

		v[i] = value
		mtf_1 = value

		for index >= 1 {
			index--
			mtf[index+1] = mtf[index]
		}

		mtf[0] = mtf_1
	}
}

/* Decodes a series of Huffman table using ReadHuffmanCode function. */
func huffmanTreeGroupDecode(group *huffmanTreeGroup, s *Reader) int {
	if s.substateTreeGroup != stateTreeGroupLoop {
		s.next = group.codes
		s.htreeIndex = 0
		s.substateTreeGroup = stateTreeGroupLoop
	}

	for s.htreeIndex < int(group.numHtrees) {
		var (
			table_size uint32
			result     = readHuffmanCode(
				uint32(group.alphabetSize),
				uint32(group.maxSymbol),
				s.next,
				&table_size,
				s,
			)
		)

		if result != decoderSuccess {
			return result
		}

		group.htrees[s.htreeIndex] = s.next
		s.next = s.next[table_size:]
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
func decodeContextMap(context_map_size uint32, num_htrees *uint32, context_map_arg *[]byte, s *Reader) int {
	var (
		br     = &s.br
		result int
	)

	switch int(s.substateContextMap) {
	case stateContextMapNone:
		result = decodeVarLenUint8(s, br, num_htrees)
		if result != decoderSuccess {
			return result
		}

		(*num_htrees)++
		s.contextIndex = 0

		*context_map_arg = make([]byte, uint(context_map_size))
		if *context_map_arg == nil {
			return decoderErrorAllocContextMap
		}

		if *num_htrees <= 1 {
			for i := 0; i < int(context_map_size); i++ {
				(*context_map_arg)[i] = 0
			}

			return decoderSuccess
		}

		s.substateContextMap = stateContextMapReadPrefix

		fallthrough

	/* Fall through. */
	case stateContextMapReadPrefix:
		{
			var bits uint32

			/* In next stage ReadHuffmanCode uses at least 4 bits, so it is safe
			   to peek 4 bits ahead. */
			if !safeGetBits(br, 5, &bits) {
				return decoderNeedsMoreInput
			}

			if bits&1 != 0 { /* Use RLE for zeros. */
				s.maxRunLengthPrefix = (bits >> 1) + 1

				dropBits(br, 5)
			} else {
				s.maxRunLengthPrefix = 0

				dropBits(br, 1)
			}

			s.substateContextMap = stateContextMapHuffman
		}

		fallthrough

		/* Fall through. */

	case stateContextMapHuffman:
		{
			alphabet_size := *num_htrees + s.maxRunLengthPrefix

			result = readHuffmanCode(alphabet_size, alphabet_size, s.contextMapTable[:], nil, s)
			if result != decoderSuccess {
				return result
			}

			s.code = 0xFFFF
			s.substateContextMap = stateContextMapDecode
		}

		fallthrough

		/* Fall through. */

	case stateContextMapDecode:
		{
			var (
				context_index         = s.contextIndex
				max_run_length_prefix = s.maxRunLengthPrefix
				context_map           = *context_map_arg
				code                  = s.code
				skip_preamble         = (code != 0xFFFF)
			)

			for context_index < context_map_size || skip_preamble {
				if !skip_preamble {
					if !safeReadSymbol(s.contextMapTable[:], br, &code) {
						s.code = 0xFFFF
						s.contextIndex = context_index
						return decoderNeedsMoreInput
					}

					if code == 0 {
						context_map[context_index] = 0
						context_index++
						continue
					}

					if code > max_run_length_prefix {
						context_map[context_index] = byte(code - max_run_length_prefix)
						context_index++
						continue
					}
				} else {
					skip_preamble = false
				}

				/* RLE sub-stage. */
				{
					var reps uint32
					if !safeReadBits(br, code, &reps) {
						s.code = code
						s.contextIndex = context_index
						return decoderNeedsMoreInput
					}

					reps += 1 << code
					if context_index+reps > context_map_size {
						return decoderErrorFormatContextMapRepeat
					}

					for {
						context_map[context_index] = 0
						context_index++

						reps--
						if reps == 0 {
							break
						}
					}
				}
			}
		}

		fallthrough

	case stateContextMapTransform:
		var bits uint32
		if !safeReadBits(br, 1, &bits) {
			s.substateContextMap = stateContextMapTransform
			return decoderNeedsMoreInput
		}

		if bits != 0 {
			inverseMoveToFrontTransform(*context_map_arg, context_map_size, s)
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
func decodeBlockTypeAndLength(safe int, s *Reader, tree_type int) bool {
	max_block_type := s.numBlockTypes[tree_type]

	type_tree := s.blockTypeTrees[tree_type*huffmanMaxSize258:]
	len_tree := s.blockLenTrees[tree_type*huffmanMaxSize26:]

	var (
		br         = &s.br
		ringbuffer = s.blockTypeRB[tree_type*2:]
		block_type uint32
	)

	if max_block_type <= 1 {
		return false
	}

	/* Read 0..15 + 3..39 bits. */
	if safe == 0 {
		block_type = readSymbol(type_tree, br)
		s.blockLength[tree_type] = readBlockLength(len_tree, br)
	} else {
		var memento bitReaderState
		bitReaderSaveState(br, &memento)

		if !safeReadSymbol(type_tree, br, &block_type) {
			return false
		}

		if !safeReadBlockLength(s, &s.blockLength[tree_type], len_tree, br) {
			s.substateReadBlockLength = stateReadBlockLengthNone

			bitReaderRestoreState(br, &memento)

			return false
		}
	}

	switch block_type {
	case 1:
		block_type = ringbuffer[1] + 1
	case 0:
		block_type = ringbuffer[0]
	default:
		block_type -= 2
	}

	if block_type >= max_block_type {
		block_type -= max_block_type
	}

	ringbuffer[0] = ringbuffer[1]
	ringbuffer[1] = block_type

	return true
}

func detectTrivialLiteralBlockTypes(s *Reader) {
	var i uint
	for i = 0; i < 8; i++ {
		s.trivialLiteralContexts[i] = 0
	}

	for i = 0; uint32(i) < s.numBlockTypes[0]; i++ {
		var (
			offset      = i << literalContextBits
			error  uint = 0
			sample      = uint(s.contextMap[offset])
			j      uint
		)

		for j = 0; j < 1<<literalContextBits; {
			var k int
			for k = 0; k < 4; k++ {
				error |= uint(s.contextMap[offset+j]) ^ sample
				j++
			}
		}

		if error == 0 {
			s.trivialLiteralContexts[i>>5] |= 1 << (i & 31)
		}
	}
}

func prepareLiteralDecoding(s *Reader) {
	var (
		context_mode   byte
		trivial        uint
		block_type     = s.blockTypeRB[1]
		context_offset = block_type << literalContextBits
	)

	s.contextMapSlice = s.contextMap[context_offset:]
	trivial = uint(s.trivialLiteralContexts[block_type>>5])
	s.trivialLiteralContext = int((trivial >> (block_type & 31)) & 1)
	s.literalHtree = []huffmanCode(s.literalHgroup.htrees[s.contextMapSlice[0]])
	context_mode = s.contextModes[block_type] & 3
	s.contextLookup = getContextLUT(int(context_mode))
}

/*
Decodes the block type and updates the state for literal context.

	Reads 3..54 bits.
*/
func decodeLiteralBlockSwitchInternal(safe int, s *Reader) bool {
	if !decodeBlockTypeAndLength(safe, s, 0) {
		return false
	}

	prepareLiteralDecoding(s)

	return true
}

func decodeLiteralBlockSwitch(s *Reader) {
	decodeLiteralBlockSwitchInternal(0, s)
}

func safeDecodeLiteralBlockSwitch(s *Reader) bool {
	return decodeLiteralBlockSwitchInternal(1, s)
}

/*
Block switch for insert/copy length.

	Reads 3..54 bits.
*/
func decodeCommandBlockSwitchInternal(safe int, s *Reader) bool {
	if !decodeBlockTypeAndLength(safe, s, 1) {
		return false
	}

	s.htreeCommand = []huffmanCode(s.insertCopyHgroup.htrees[s.blockTypeRB[3]])

	return true
}

func decodeCommandBlockSwitch(s *Reader) {
	decodeCommandBlockSwitchInternal(0, s)
}

func safeDecodeCommandBlockSwitch(s *Reader) bool {
	return decodeCommandBlockSwitchInternal(1, s)
}

/*
Block switch for distance codes.

	Reads 3..54 bits.
*/
func decodeDistanceBlockSwitchInternal(safe int, s *Reader) bool {
	if !decodeBlockTypeAndLength(safe, s, 2) {
		return false
	}

	s.distContextMapSlice = s.distContextMap[s.blockTypeRB[5]<<distanceContextBits:]
	s.distHtreeIndex = s.distContextMapSlice[s.distanceContext]

	return true
}

func decodeDistanceBlockSwitch(s *Reader) {
	decodeDistanceBlockSwitchInternal(0, s)
}

func safeDecodeDistanceBlockSwitch(s *Reader) bool {
	return decodeDistanceBlockSwitchInternal(1, s)
}

func unwrittenBytes(s *Reader, wrap bool) uint {
	var pos uint
	if wrap && s.pos > s.ringbufferSize {
		pos = uint(s.ringbufferSize)
	} else {
		pos = uint(s.pos)
	}

	partial_pos_rb := (s.rbRoundtrips * uint(s.ringbufferSize)) + pos

	return partial_pos_rb - s.partialPosOut
}

/*
Dumps output.

	Returns BROTLI_DECODER_NEEDS_MORE_OUTPUT only if there is more output to push
	and either ring-buffer is as big as window size, or |force| is true.
*/
func writeRingBuffer(s *Reader, available_out *uint, next_out *[]byte, total_out *uint, force bool) int {
	start := s.ringbuffer[s.partialPosOut&uint(s.ringbufferMask):]

	var (
		to_write    = unwrittenBytes(s, true)
		num_written = *available_out
	)

	if num_written > to_write {
		num_written = to_write
	}

	if s.metaBlockRemainingLen < 0 {
		return decoderErrorFormatBlockLength1
	}

	if next_out != nil && *next_out == nil {
		*next_out = start
	} else if next_out != nil {
		copy(*next_out, start[:num_written])
		*next_out = (*next_out)[num_written:]
	}

	*available_out -= num_written

	s.partialPosOut += num_written
	if total_out != nil {
		*total_out = s.partialPosOut
	}

	if num_written < to_write {
		if s.ringbufferSize == 1<<s.windowBits || force {
			return decoderNeedsMoreOutput
		} else {
			return decoderSuccess
		}
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

func wrapRingBuffer(s *Reader) {
	if s.shouldWrapRingbuffer != 0 {
		copy(s.ringbuffer, s.ringbufferEnd[:uint(s.pos)])
		s.shouldWrapRingbuffer = 0
	}
}

/*
Allocates ring-buffer.

	s->ringbuffer_size MUST be updated by BrotliCalculateRingBufferSize before
	this function is called.

	Last two bytes of ring-buffer are initialized to 0, so context calculation
	could be done uniformly for the first two and all other positions.
*/
func ensureRingBuffer(s *Reader) bool {
	var old_ringbuffer []byte

	if s.ringbufferSize == s.newRingbufferSize {
		return true
	}

	spaceNeeded := int(s.newRingbufferSize) + int(kRingBufferWriteAheadSlack)
	if len(s.ringbuffer) < spaceNeeded {
		old_ringbuffer = s.ringbuffer
		s.ringbuffer = make([]byte, spaceNeeded)
	}

	s.ringbuffer[s.newRingbufferSize-2] = 0
	s.ringbuffer[s.newRingbufferSize-1] = 0

	if old_ringbuffer != nil {
		copy(s.ringbuffer, old_ringbuffer[:uint(s.pos)])
	}

	s.ringbufferSize = s.newRingbufferSize
	s.ringbufferMask = s.newRingbufferSize - 1
	s.ringbufferEnd = s.ringbuffer[s.ringbufferSize:]

	return true
}

func copyUncompressedBlockToOutput(available_out *uint, next_out *[]byte, total_out *uint, s *Reader) int {
	/* TODO: avoid allocation for single uncompressed block. */
	if !ensureRingBuffer(s) {
		return decoderErrorAllocRingBuffer1
	}

	/* State machine */
	for {
		switch s.substateUncompressed {
		case stateUncompressedNone:
			{
				nbytes := int(getRemainingBytes(&s.br))
				if nbytes > s.metaBlockRemainingLen {
					nbytes = s.metaBlockRemainingLen
				}

				if s.pos+nbytes > s.ringbufferSize {
					nbytes = s.ringbufferSize - s.pos
				}

				/* Copy remaining bytes from s->br.buf_ to ring-buffer. */
				copyBytes(s.ringbuffer[s.pos:], &s.br, uint(nbytes))

				s.pos += nbytes

				s.metaBlockRemainingLen -= nbytes
				if s.pos < 1<<s.windowBits {
					if s.metaBlockRemainingLen == 0 {
						return decoderSuccess
					}

					return decoderNeedsMoreInput
				}

				s.substateUncompressed = stateUncompressedWrite
			}

			fallthrough

		case stateUncompressedWrite:
			{
				result := writeRingBuffer(s, available_out, next_out, total_out, false)
				if result != decoderSuccess {
					return result
				}

				if s.ringbufferSize == 1<<s.windowBits {
					s.maxDistance = s.maxBackwardDistance
				}

				s.substateUncompressed = stateUncompressedNone

				break
			}
		}
	}
}

/*
Calculates the smallest feasible ring buffer.

	If we know the data size is small, do not allocate more ring buffer
	size than needed to reduce memory usage.

	When this method is called, metablock size and flags MUST be decoded.
*/
func calculateRingBufferSize(s *Reader) {
	var (
		window_size         = 1 << s.windowBits
		new_ringbuffer_size = window_size
		min_size            int
	)
	/* We need at least 2 bytes of ring buffer size to get the last two
	   bytes for context from there */

	if s.ringbufferSize != 0 {
		min_size = s.ringbufferSize
	} else {
		min_size = 1024
	}

	var output_size int

	/* If maximum is already reached, no further extension is retired. */
	if s.ringbufferSize == window_size {
		return
	}

	/* Metadata blocks does not touch ring buffer. */
	if s.isMetadata != 0 {
		return
	}

	if s.ringbuffer == nil {
		output_size = 0
	} else {
		output_size = s.pos
	}

	output_size += s.metaBlockRemainingLen
	if min_size < output_size {
		min_size = output_size
	}

	if s.cannyRingbufferAllocation != 0 {
		/* Reduce ring buffer size to save memory when server is unscrupulous.
		   In worst case memory usage might be 1.5x bigger for a short period of
		   ring buffer reallocation. */
		for new_ringbuffer_size>>1 >= min_size {
			new_ringbuffer_size >>= 1
		}
	}

	s.newRingbufferSize = new_ringbuffer_size
}

/* Reads 1..256 2-bit context modes. */
func readContextModes(s *Reader) int {
	var (
		br = &s.br
		i  = s.loopCounter
	)

	for i < int(s.numBlockTypes[0]) {
		var bits uint32
		if !safeReadBits(br, 2, &bits) {
			s.loopCounter = i
			return decoderNeedsMoreInput
		}

		s.contextModes[i] = byte(bits)
		i++
	}

	return decoderSuccess
}

func takeDistanceFromRingBuffer(s *Reader) {
	if s.distanceCode == 0 {
		s.distRbIdx--
		s.distanceCode = s.distRb[s.distRbIdx&3]

		/* Compensate double distance-ring-buffer roll for dictionary items. */
		s.distanceContext = 1
	} else {
		distance_code := s.distanceCode << 1

		const (
			kDistanceShortCodeIndexOffset uint32 = 0xAAAFFF1B
			kDistanceShortCodeValueOffset uint32 = 0xFA5FA500
		)

		v := (s.distRbIdx + int(kDistanceShortCodeIndexOffset>>uint(distance_code))) & 0x3
		/* kDistanceShortCodeIndexOffset has 2-bit values from LSB:
		   3, 2, 1, 0, 3, 3, 3, 3, 3, 3, 2, 2, 2, 2, 2, 2 */

		/* kDistanceShortCodeValueOffset has 2-bit values from LSB:
		   -0, 0,-0, 0,-1, 1,-2, 2,-3, 3,-1, 1,-2, 2,-3, 3 */
		s.distanceCode = s.distRb[v]

		v = int(kDistanceShortCodeValueOffset>>uint(distance_code)) & 0x3
		if distance_code&0x3 != 0 {
			s.distanceCode += v
		} else {
			s.distanceCode -= v
			if s.distanceCode <= 0 {
				/* A huge distance will cause a () soon.
				   This is a little faster than failing here. */
				s.distanceCode = 0x7FFFFFFF
			}
		}
	}
}

func safeReadBitsMaybeZero(br *bitReader, n_bits uint32, val *uint32) bool {
	if n_bits != 0 {
		return safeReadBits(br, n_bits, val)
	} else {
		*val = 0
		return true
	}
}

/* Precondition: s->distance_code < 0. */
func readDistanceInternal(safe int, s *Reader, br *bitReader) bool {
	var (
		distval       int
		memento       bitReaderState
		distance_tree = []huffmanCode(s.distanceHgroup.htrees[s.distHtreeIndex])
	)

	if safe == 0 {
		s.distanceCode = int(readSymbol(distance_tree, br))
	} else {
		var code uint32

		bitReaderSaveState(br, &memento)

		if !safeReadSymbol(distance_tree, br, &code) {
			return false
		}

		s.distanceCode = int(code)
	}

	/* Convert the distance code to the actual distance by possibly
	   looking up past distances from the s->ringbuffer. */
	s.distanceContext = 0

	if s.distanceCode&^0xF == 0 {
		takeDistanceFromRingBuffer(s)
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
			s.distanceCode = int(s.numDirectDistanceCodes) + offset + int(readBits(br, nbits))
		} else {
			/* This branch also works well when s->distance_postfix_bits == 0. */
			var bits uint32

			postfix = distval & s.distancePostfixMask
			distval >>= s.distancePostfixBits

			nbits = (uint32(distval) >> 1) + 1
			if safe != 0 {
				if !safeReadBitsMaybeZero(br, nbits, &bits) {
					s.distanceCode = -1 /* Restore precondition. */

					bitReaderRestoreState(br, &memento)

					return false
				}
			} else {
				bits = readBits(br, nbits)
			}

			offset = ((2 + (distval & 1)) << nbits) - 4
			s.distanceCode = int(s.numDirectDistanceCodes) + ((offset + int(bits)) << s.distancePostfixBits) + postfix
		}
	}

	s.distanceCode = s.distanceCode - numDistanceShortCodes + 1
	s.blockLength[2]--

	return true
}

func readDistance(s *Reader, br *bitReader) {
	readDistanceInternal(0, s, br)
}

func safeReadDistance(s *Reader, br *bitReader) bool {
	return readDistanceInternal(1, s, br)
}

func readCommandInternal(safe int, s *Reader, br *bitReader, insert_length *int) bool {
	var (
		cmd_code         uint32
		insert_len_extra uint32 = 0
		copy_length      uint32
		v                cmdLutElement
		memento          bitReaderState
	)

	if safe == 0 {
		cmd_code = readSymbol(s.htreeCommand, br)
	} else {
		bitReaderSaveState(br, &memento)

		if !safeReadSymbol(s.htreeCommand, br, &cmd_code) {
			return false
		}
	}

	v = kCmdLut[cmd_code]
	s.distanceCode = int(v.distanceCode)
	s.distanceContext = int(v.context)
	s.distHtreeIndex = s.distContextMapSlice[s.distanceContext]

	*insert_length = int(v.insertLenOffset)
	if safe == 0 {
		if v.insertLenExtraBits != 0 {
			insert_len_extra = readBits(br, uint32(v.insertLenExtraBits))
		}

		copy_length = readBits(br, uint32(v.copyLenExtraBits))
	} else if !safeReadBitsMaybeZero(br, uint32(v.insertLenExtraBits), &insert_len_extra) ||
		!safeReadBitsMaybeZero(br, uint32(v.copyLenExtraBits), &copy_length) {
		bitReaderRestoreState(br, &memento)
		return false
	}

	s.copyLength = int(copy_length) + int(v.copyLenOffset)
	s.blockLength[1]--
	*insert_length += int(insert_len_extra)

	return true
}

func readCommand(s *Reader, br *bitReader, insert_length *int) {
	readCommandInternal(0, s, br, insert_length)
}

func safeReadCommand(s *Reader, br *bitReader, insert_length *int) bool {
	return readCommandInternal(1, s, br, insert_length)
}

func checkInputAmountMaybeSafe(safe int, br *bitReader, num uint) bool {
	if safe != 0 {
		return true
	}

	return checkInputAmount(br, num)
}

func processCommandsInternal(safe int, s *Reader) int {
	var (
		pos    = s.pos
		i      = s.loopCounter
		result = decoderSuccess
		br     = &s.br
		hc     []huffmanCode
	)

	if !checkInputAmountMaybeSafe(safe, br, 28) {
		result = decoderNeedsMoreInput
		goto saveStateAndReturn
	}

	if safe == 0 {
		warmupBitReader(br)
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

	if !checkInputAmountMaybeSafe(safe, br, 28) { /* 156 bits + 7 bytes */
		s.state = stateCommandBegin
		result = decoderNeedsMoreInput
		goto saveStateAndReturn
	}

	if s.blockLength[1] == 0 {
		if safe != 0 {
			if !safeDecodeCommandBlockSwitch(s) {
				result = decoderNeedsMoreInput
				goto saveStateAndReturn
			}
		} else {
			decodeCommandBlockSwitch(s)
		}

		goto CommandBegin
	}

	/* Read the insert/copy length in the command. */
	if safe != 0 {
		if !safeReadCommand(s, br, &i) {
			result = decoderNeedsMoreInput
			goto saveStateAndReturn
		}
	} else {
		readCommand(s, br, &i)
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
			if !checkInputAmountMaybeSafe(safe, br, 28) { /* 162 bits + 7 bytes */
				s.state = stateCommandInner
				result = decoderNeedsMoreInput
				goto saveStateAndReturn
			}

			if s.blockLength[0] == 0 {
				if safe != 0 {
					if !safeDecodeLiteralBlockSwitch(s) {
						result = decoderNeedsMoreInput
						goto saveStateAndReturn
					}
				} else {
					decodeLiteralBlockSwitch(s)
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
		var (
			p1 = s.ringbuffer[(pos-1)&s.ringbufferMask]
			p2 = s.ringbuffer[(pos-2)&s.ringbufferMask]
		)

		for {
			var context byte

			if !checkInputAmountMaybeSafe(safe, br, 28) { /* 162 bits + 7 bytes */
				s.state = stateCommandInner
				result = decoderNeedsMoreInput
				goto saveStateAndReturn
			}

			if s.blockLength[0] == 0 {
				if safe != 0 {
					if !safeDecodeLiteralBlockSwitch(s) {
						result = decoderNeedsMoreInput
						goto saveStateAndReturn
					}
				} else {
					decodeLiteralBlockSwitch(s)
				}

				if s.trivialLiteralContext != 0 {
					goto CommandInner
				}
			}

			context = getContext(p1, p2, s.contextLookup)
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
				if !safeDecodeDistanceBlockSwitch(s) {
					result = decoderNeedsMoreInput
					goto saveStateAndReturn
				}
			} else {
				decodeDistanceBlockSwitch(s)
			}
		}

		if safe != 0 {
			if !safeReadDistance(s, br) {
				result = decoderNeedsMoreInput
				goto saveStateAndReturn
			}
		} else {
			readDistance(s, br)
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
		/* The maximum allowed distance is BROTLI_MAX_ALLOWED_DISTANCE = 0x7FFFFFFC.
		   With this choice, no signed overflow can occur after decoding
		   a special distance code (e.g., after adding 3 to the last distance). */
		if s.distanceCode > maxAllowedDistance {
			return decoderErrorFormatDistance
		}

		if i >= minDictionaryWordLength && i <= maxDictionaryWordLength {
			var (
				address       = s.distanceCode - s.maxDistance - 1
				words         = s.dictionary
				trans         = s.transforms
				offset        = int(s.dictionary.offsets_by_length[i])
				shift         = uint32(s.dictionary.size_bits_by_length[i])
				mask          = int(bitMask(shift))
				word_idx      = address & mask
				transform_idx = address >> shift
			)

			/* Compensate double distance-ring-buffer roll. */
			s.distRbIdx += s.distanceContext

			offset += word_idx * i

			if words.data == nil {
				return decoderErrorDictionaryNotSet
			}

			if transform_idx < int(trans.numTransforms) {
				word := words.data[offset:]

				len := i
				if transform_idx == int(trans.cutOffTransforms[0]) {
					copy(s.ringbuffer[pos:], word[:uint(len)])
				} else {
					len = transformDictionaryWord(s.ringbuffer[pos:], word, int(len), trans, transform_idx)
				}

				pos += int(len)

				s.metaBlockRemainingLen -= int(len)
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
		src_start := (pos - s.distanceCode) & s.ringbufferMask

		copy_dst := s.ringbuffer[pos:]
		copy_src := s.ringbuffer[src_start:]

		var (
			dst_end = pos + i
			src_end = src_start + i
		)

		/* Update the recent distances cache. */
		s.distRb[s.distRbIdx&3] = s.distanceCode

		s.distRbIdx++
		s.metaBlockRemainingLen -= i

		/* There are 32+ bytes of slack in the ring-buffer allocation.
		   Also, we have 16 short codes, that make these 16 bytes irrelevant
		   in the ring-buffer. Let's copy over them as a first guess. */
		copy(copy_dst, copy_src[:16])

		if src_end > pos && dst_end > src_start {
			/* Regions intersect. */
			goto CommandPostWrapCopy
		}

		if dst_end >= s.ringbufferSize || src_end >= s.ringbufferSize {
			/* At least one region wraps. */
			goto CommandPostWrapCopy
		}

		pos += i
		if i > 16 {
			if i > 32 {
				copy(copy_dst[16:], copy_src[16:][:uint(i-16)])
			} else {
				/* This branch covers about 45% cases.
				   Fixed size short copy allows more compiler optimizations. */
				copy(copy_dst[16:], copy_src[16:][:16])
			}
		}
	}

	if s.metaBlockRemainingLen <= 0 {
		/* Next metablock, if any. */
		s.state = stateMetablockDone

		goto saveStateAndReturn
	} else {
		goto CommandBegin
	}

CommandPostWrapCopy:
	{
		wrap_guard := s.ringbufferSize - pos
		for {
			i--
			if i < 0 {
				break
			}

			s.ringbuffer[pos] = s.ringbuffer[(pos-s.distanceCode)&s.ringbufferMask]
			pos++

			wrap_guard--
			if wrap_guard == 0 {
				s.state = stateCommandPostWrite2
				goto saveStateAndReturn
			}
		}
	}

	if s.metaBlockRemainingLen <= 0 {
		/* Next metablock, if any. */
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

func processCommands(s *Reader) int {
	return processCommandsInternal(0, s)
}

func safeProcessCommands(s *Reader) int {
	return processCommandsInternal(1, s)
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
func decoderDecompressStream(
	s *Reader,
	available_in *uint,
	next_in *[]byte,
	available_out *uint,
	next_out *[]byte,
) int {
	var (
		result = decoderSuccess
		br     = &s.br
	)

	/* Do not try to process further in a case of unrecoverable error. */

	if int(s.errorCode) < 0 {
		return decoderResultError
	}

	if *available_out != 0 && (next_out == nil || *next_out == nil) {
		return saveErrorCode(s, decoderErrorInvalidArguments)
	}

	if *available_out == 0 {
		next_out = nil
	}

	if s.bufferLength == 0 { /* Just connect bit reader to input stream. */
		br.inputLen = *available_in
		br.input = *next_in
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
					intermediate_result := writeRingBuffer(s, available_out, next_out, nil, true)

					/* WriteRingBuffer checks s->meta_block_remaining_len validity. */
					if int(intermediate_result) < 0 {
						result = intermediate_result
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

						br.inputLen = *available_in
						br.input = *next_in
						br.bytePos = 0

						continue
					} else if *available_in != 0 {
						/* Not enough data in buffer, but can take one more byte from
						   input stream. */
						result = decoderSuccess

						s.buffer.u8[s.bufferLength] = (*next_in)[0]
						s.bufferLength++
						br.inputLen = uint(s.bufferLength)
						*next_in = (*next_in)[1:]
						(*available_in)--

						/* Retry with more data in buffer. */
						continue
					}

					/* Can't finish reading and no more input. */
					break
					/* Input stream doesn't contain enough input. */
				} else {
					/* Copy tail to internal buffer and return. */
					*next_in = br.input[br.bytePos:]

					*available_in = br.inputLen - br.bytePos
					for *available_in != 0 {
						s.buffer.u8[s.bufferLength] = (*next_in)[0]
						s.bufferLength++
						*next_in = (*next_in)[1:]
						(*available_in)--
					}

					break
				}
			}

			/* Unreachable. */

			/* Fail or needs more output. */
			if s.bufferLength != 0 {
				/* Just consumed the buffered input and produced some output. Otherwise
				   it would result in "needs more input". Reset internal buffer. */
				s.bufferLength = 0
			} else {
				/* Using input stream in last iteration. When decoder switches to input
				   stream it has less than 8 bits in accumulator, so it is safe to
				   return unused accumulator bits there. */
				bitReaderUnload(br)

				*available_in = br.inputLen - br.bytePos
				*next_in = br.input[br.bytePos:]
			}

			break
		}

		switch s.state {
		/* Prepare to the first read. */
		case stateUninited:
			if !warmupBitReader(br) {
				result = decoderNeedsMoreInput
				break
			}

			/* Decode window size. */
			result = decodeWindowBits(s, br) /* Reads 1..8 bits. */
			if result != decoderSuccess {
				break
			}

			if s.largeWindow {
				s.state = stateLargeWindowBits
				break
			}

			s.state = stateInitialize

		case stateLargeWindowBits:
			if !safeReadBits(br, 6, &s.windowBits) {
				result = decoderNeedsMoreInput
				break
			}

			if s.windowBits < largeMinWbits || s.windowBits > largeMaxWbits {
				result = decoderErrorFormatWindowBits
				break
			}

			s.state = stateInitialize

			fallthrough

			/* Maximum distance, see section 9.1. of the spec. */

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

			/* Fall through. */

		case stateMetablockBegin:
			decoderStateMetablockBegin(s)

			s.state = stateMetablockHeader

			fallthrough

			/* Fall through. */

		case stateMetablockHeader:
			result = decodeMetaBlockLength(s, br)
			/* Reads 2 - 31 bits. */
			if result != decoderSuccess {
				break
			}

			if s.isMetadata != 0 || s.isUncompressed != 0 {
				if !bitReaderJumpToByteBoundary(br) {
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

			calculateRingBufferSize(s)

			if s.isUncompressed != 0 {
				s.state = stateUncompressed
				break
			}

			s.loopCounter = 0
			s.state = stateHuffmanCode0

		case stateUncompressed:
			result = copyUncompressedBlockToOutput(available_out, next_out, nil, s)
			if result == decoderSuccess {
				s.state = stateMetablockDone
			}

		case stateMetadata:
			for ; s.metaBlockRemainingLen > 0; s.metaBlockRemainingLen-- {
				var bits uint32

				/* Read one byte and ignore it. */
				if !safeReadBits(br, 8, &bits) {
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
			result = decodeVarLenUint8(s, br, &s.numBlockTypes[s.loopCounter])

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
				var (
					alphabet_size = s.numBlockTypes[s.loopCounter] + 2
					tree_offset   = s.loopCounter * huffmanMaxSize258
				)

				result = readHuffmanCode(alphabet_size, alphabet_size, s.blockTypeTrees[tree_offset:], nil, s)
				if result != decoderSuccess {
					break
				}

				s.state = stateHuffmanCode2
			}

			fallthrough

		case stateHuffmanCode2:
			{
				var (
					alphabet_size uint32 = numBlockLenSymbols
					tree_offset          = s.loopCounter * huffmanMaxSize26
				)

				result = readHuffmanCode(alphabet_size, alphabet_size, s.blockLenTrees[tree_offset:], nil, s)
				if result != decoderSuccess {
					break
				}

				s.state = stateHuffmanCode3
			}

			fallthrough

		case stateHuffmanCode3:
			tree_offset := s.loopCounter * huffmanMaxSize26
			if !safeReadBlockLength(s, &s.blockLength[s.loopCounter], s.blockLenTrees[tree_offset:], br) {
				result = decoderNeedsMoreInput
				break
			}

			s.loopCounter++
			s.state = stateHuffmanCode0

		case stateMetablockHeader2:
			{
				var bits uint32
				if !safeReadBits(br, 6, &bits) {
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
			result = readContextModes(s)

			if result != decoderSuccess {
				break
			}

			s.state = stateContextMap1

			fallthrough

		case stateContextMap1:
			result = decodeContextMap(s.numBlockTypes[0]<<literalContextBits, &s.numLiteralHtrees, &s.contextMap, s)

			if result != decoderSuccess {
				break
			}

			detectTrivialLiteralBlockTypes(s)
			s.state = stateContextMap2

			fallthrough

		case stateContextMap2:
			{
				var (
					num_direct_codes    = s.numDirectDistanceCodes - numDistanceShortCodes
					num_distance_codes  uint32
					max_distance_symbol uint32
				)

				if s.largeWindow {
					num_distance_codes = distanceAlphabetSize(
						s.distancePostfixBits,
						num_direct_codes,
						largeMaxDistanceBits,
					)
					max_distance_symbol = maxDistanceSymbol(num_direct_codes, s.distancePostfixBits)
				} else {
					num_distance_codes = distanceAlphabetSize(s.distancePostfixBits, num_direct_codes, maxDistanceBits)
					max_distance_symbol = num_distance_codes
				}

				allocation_success := true

				result = decodeContextMap(
					s.numBlockTypes[2]<<distanceContextBits,
					&s.numDistHtrees,
					&s.distContextMap,
					s,
				)
				if result != decoderSuccess {
					break
				}

				decoderHuffmanTreeGroupInit(&s.literalHgroup, numLiteralSymbols, numLiteralSymbols, s.numLiteralHtrees)
				decoderHuffmanTreeGroupInit(
					&s.insertCopyHgroup,
					numCommandSymbols,
					numCommandSymbols,
					s.numBlockTypes[1],
				)
				decoderHuffmanTreeGroupInit(&s.distanceHgroup, num_distance_codes, max_distance_symbol, s.numDistHtrees)

				if !allocation_success {
					return saveErrorCode(s, decoderErrorAllocTreeGroups)
				}

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
				return saveErrorCode(s, decoderErrorUnreachable)
			}

			result = huffmanTreeGroupDecode(hgroup, s)
			if result != decoderSuccess {
				break
			}

			s.loopCounter++
			if s.loopCounter >= 3 {
				prepareLiteralDecoding(s)
				s.distContextMapSlice = s.distContextMap

				s.htreeCommand = []huffmanCode(s.insertCopyHgroup.htrees[0])
				if !ensureRingBuffer(s) {
					result = decoderErrorAllocRingBuffer2
					break
				}

				s.state = stateCommandBegin
			}

		case stateCommandBegin, stateCommandInner, stateCommandPostDecodeLiterals, stateCommandPostWrapCopy:
			result = processCommands(s)

			if result == decoderNeedsMoreInput {
				result = safeProcessCommands(s)
			}

		case stateCommandInnerWrite, stateCommandPostWrite1, stateCommandPostWrite2:
			result = writeRingBuffer(s, available_out, next_out, nil, false)

			if result != decoderSuccess {
				break
			}

			wrapRingBuffer(s)

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

			decoderStateCleanupAfterMetablock(s)

			if s.isLastMetablock == 0 {
				s.state = stateMetablockBegin
				break
			}

			if !bitReaderJumpToByteBoundary(br) {
				result = decoderErrorFormatPadding2
				break
			}

			if s.bufferLength == 0 {
				bitReaderUnload(br)
				*available_in = br.inputLen - br.bytePos
				*next_in = br.input[br.bytePos:]
			}

			s.state = stateDone

			fallthrough

		case stateDone:
			if s.ringbuffer != nil {
				result = writeRingBuffer(s, available_out, next_out, nil, true)
				if result != decoderSuccess {
					break
				}
			}

			return saveErrorCode(s, result)
		}
	}

	return saveErrorCode(s, result)
}

func decoderHasMoreOutput(s *Reader) bool {
	/* After unrecoverable error remaining output is considered nonsensical. */
	if int(s.errorCode) < 0 {
		return false
	}

	return s.ringbuffer != nil && unwrittenBytes(s, false) != 0
}

func decoderGetErrorCode(s *Reader) int {
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
