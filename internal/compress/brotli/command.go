// Copyright 2016 Google Inc. All Rights Reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli

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
