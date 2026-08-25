// Copyright 2016 Google Inc. All Rights Reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli

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

			reqBlockTypeTreesLen := 3 * (huffmanMaxSize258 + huffmanMaxSize26)
			if cap(s.blockTypeTrees) < reqBlockTypeTreesLen {
				/* Allocate memory for both block_type_trees and block_len_trees. */
				s.blockTypeTrees = make([]huffmanCode, reqBlockTypeTreesLen)
			} else {
				s.blockTypeTrees = s.blockTypeTrees[:reqBlockTypeTreesLen]
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

			if s.isMetadata || s.isUncompressed {
				if !br.jumpToByteBoundary() {
					result = decoderErrorFormatPadding1
					break
				}
			}

			if s.isMetadata {
				s.state = stateMetadata
				break
			}

			if s.metaBlockRemainingLen == 0 {
				s.state = stateMetablockDone
				break
			}

			s.calculateRingBufferSize()

			if s.isUncompressed {
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

				if cap(s.contextModes) < int(s.numBlockTypes[0]) {
					s.contextModes = make([]byte, s.numBlockTypes[0])
				} else {
					s.contextModes = s.contextModes[:s.numBlockTypes[0]]
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

			if !s.isLastMetablock {
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
