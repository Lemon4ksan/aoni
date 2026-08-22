// Copyright 2016 Google Inc. All Rights Reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli

/*
We need the slack region for the following reasons:
  - doing up to two 16-byte copies for fast backward copying
  - inserting transformed dictionary word (5 prefix + 24 base + 8 suffix)
*/
const kRingBufferWriteAheadSlack uint32 = 42

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
Returns decoderNeedsMoreOutput only if there is more output to push
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
		s.shouldWrapRingbuffer = uint(s.pos) != 0
	}

	return decoderSuccess
}

func (s *Reader) wrapRingBuffer() {
	if s.shouldWrapRingbuffer {
		copy(s.ringbuffer, s.ringbufferEnd[:uint(s.pos)])
		s.shouldWrapRingbuffer = false
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
	if cap(s.ringbuffer) < spaceNeeded {
		oldRingbuffer = s.ringbuffer
		s.ringbuffer = make([]byte, spaceNeeded)
	} else {
		s.ringbuffer = s.ringbuffer[:spaceNeeded]
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
	if s.isMetadata {
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

	if s.cannyRingbufferAllocation {
		for newRingbufferSize>>1 >= minSize {
			newRingbufferSize >>= 1
		}
	}

	s.newRingbufferSize = newRingbufferSize
}
