// Copyright 2016 The Go Authors. All rights reserved.
// Copyright (c) 2016-2023 Klaus Post. All rights reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flate

import (
	"github.com/lemon4ksan/foundation/silicon/endian"
)

// dictDecoder implements the LZ77 sliding dictionary as used in decompression.
// LZ77 decompresses data through sequences of two forms of commands:
//
//   - Literal insertions: Runs of one or more symbols are inserted into the data
//     stream as is. This is accomplished through the writeByte method for a
//     single symbol, or combinations of writeSlice/writeMark for multiple symbols.
//     Any valid stream must start with a literal insertion if no preset dictionary
//     is used.
//
//   - Backward copies: Runs of one or more symbols are copied from previously
//     emitted data. Backward copies come as the tuple (dist, length) where dist
//     determines how far back in the stream to copy from and length determines how
//     many bytes to copy. Note that it is valid for the length to be greater than
//     the distance. Since LZ77 uses forward copies, that situation is used to
//     perform a form of run-length encoding on repeated runs of symbols.
//     The writeCopy and tryWriteCopy are used to implement this command.
//
// For performance reasons, this implementation performs little to no sanity
// checks about the arguments. As such, the invariants documented for each
// method call must be respected.
type dictDecoder struct {
	hist []byte // Sliding window history

	// Invariant: 0 <= rdPos <= wrPos <= len(hist)
	wrPos   int   // Current output position in buffer
	rdPos   int   // Have emitted hist[:rdPos] already
	flushed int64 // Total bytes returned by readFlush since init
	full    bool  // Has a full window length been written yet?
}

// init initializes dictDecoder to have a sliding window dictionary of the given
// size. If a preset dict is provided, it will initialize the dictionary with
// the contents of dict.
func (dd *dictDecoder) init(size int, dict []byte) {
	*dd = dictDecoder{hist: dd.hist}

	if cap(dd.hist) < size {
		dd.hist = make([]byte, size)
	}

	dd.hist = dd.hist[:size]

	if len(dict) > len(dd.hist) {
		dict = dict[len(dict)-len(dd.hist):]
	}

	dd.wrPos = copy(dd.hist, dict)
	if dd.wrPos == len(dd.hist) {
		dd.wrPos = 0
		dd.full = true
	}

	dd.rdPos = dd.wrPos
}

// histSize reports the total amount of historical data in the dictionary.
func (dd *dictDecoder) histSize() int {
	if dd.full {
		return len(dd.hist)
	}

	return dd.wrPos
}

// availRead reports the number of bytes that can be flushed by readFlush.
func (dd *dictDecoder) availRead() int {
	return dd.wrPos - dd.rdPos
}

// availWrite reports the available amount of output buffer space.
func (dd *dictDecoder) availWrite() int {
	return len(dd.hist) - dd.wrPos
}

// writeSlice returns a slice of the available buffer to write data to.
//
// This invariant will be kept: len(s) <= availWrite()
func (dd *dictDecoder) writeSlice() []byte {
	return dd.hist[dd.wrPos:]
}

// writeMark advances the writer pointer by cnt.
//
// This invariant must be kept: 0 <= cnt <= availWrite()
func (dd *dictDecoder) writeMark(cnt int) {
	dd.wrPos += cnt
}

// writeByte writes a single byte to the dictionary.
//
// This invariant must be kept: 0 < availWrite()
func (dd *dictDecoder) writeByte(c byte) {
	dd.hist[dd.wrPos] = c
	dd.wrPos++
}

// writeCopy copies a string at a given (dist, length) to the output.
// This returns the number of bytes copied and may be less than the requested
// length if the available space in the output buffer is too small.
//
// This invariant must be kept: 0 < dist <= histSize()
func (dd *dictDecoder) writeCopy(dist, length int) int {
	dstBase := dd.wrPos
	dstPos := dstBase
	srcPos := dstPos - dist
	endPos := min(dstPos+length, len(dd.hist))

	// Copy non-overlapping section after destination position.
	if srcPos < 0 {
		srcPos += len(dd.hist)
		dstPos += copy(dd.hist[dstPos:endPos], dd.hist[srcPos:])
		srcPos = 0
	}

	// RLE fast path for distance 1
	if dist == 1 && dstPos < endPos {
		c := dd.hist[srcPos]
		slice := dd.hist[dstPos:endPos]
		for i := range slice {
			slice[i] = c
		}
		dd.wrPos = endPos

		return endPos - dstBase
	}

	// Non-overlapping fast path
	if dstPos-srcPos >= endPos-dstPos {
		copy(dd.hist[dstPos:endPos], dd.hist[srcPos:srcPos+(endPos-dstPos)])
		dd.wrPos = endPos

		return endPos - dstBase
	}

	for dstPos < endPos {
		dstPos += copy(dd.hist[dstPos:endPos], dd.hist[srcPos:dstPos])
	}

	dd.wrPos = dstPos

	return dstPos - dstBase
}

// tryWriteCopy tries to copy a string at a given (distance, length) to the
// output. This specialized version is optimized for short distances.
//
// This method is designed to be inlined for performance reasons.
//
// This invariant must be kept: 0 < dist <= histSize()
//
//go:inline
func (dd *dictDecoder) tryWriteCopy(dist, length int) int {
	dstPos := dd.wrPos

	endPos := dstPos + length
	if dstPos < dist || endPos > len(dd.hist) {
		return 0
	}

	dstBase := dstPos
	srcPos := dstPos - dist

	// RLE fast path for single-byte repeats
	if dist == 1 {
		c := dd.hist[srcPos]
		slice := dd.hist[dstPos:endPos]
		for i := range slice {
			slice[i] = c
		}
		dd.wrPos = endPos

		return length
	}

	// Non-overlapping match copy with 64-bit and 128-bit wildcopy
	if dist >= length {
		if length <= 8 {
			endian.Store64(dd.hist, dstPos, endian.Load64(dd.hist, srcPos))
			dd.wrPos = endPos

			return length
		}

		if length <= 16 && dist >= 16 {
			endian.Store64(dd.hist, dstPos, endian.Load64(dd.hist, srcPos))
			endian.Store64(dd.hist, dstPos+8, endian.Load64(dd.hist, srcPos+8))
			dd.wrPos = endPos

			return length
		}

		copy(dd.hist[dstPos:endPos], dd.hist[srcPos:srcPos+length])
		dd.wrPos = endPos

		return length
	}

	// 2-byte and 4-byte repeating pattern fast paths
	if dist == 2 {
		v := endian.Load16(dd.hist, srcPos)
		for dstPos+2 <= endPos {
			endian.Store16(dd.hist[dstPos:], v)
			dstPos += 2
		}
		if dstPos < endPos {
			dd.hist[dstPos] = byte(v)
		}
		dd.wrPos = endPos

		return length
	}

	if dist == 4 {
		v := endian.Load32(dd.hist, srcPos)
		for dstPos+4 <= endPos {
			endian.Store32(dd.hist[dstPos:], v)
			dstPos += 4
		}
		for dstPos < endPos {
			dd.hist[dstPos] = dd.hist[dstPos-4]
			dstPos++
		}
		dd.wrPos = endPos

		return length
	}

	// Copy possibly overlapping section before destination position.
loop:
	dstPos += copy(dd.hist[dstPos:endPos], dd.hist[srcPos:dstPos])

	if dstPos < endPos {
		goto loop // Avoid for-loop so that this function can be inlined
	}

	dd.wrPos = dstPos

	return dstPos - dstBase
}

// appendWindow appends the current sliding window (up to len(hist) most recent
// bytes, oldest first) to dst.
func (dd *dictDecoder) appendWindow(dst []byte) []byte {
	if dd.full {
		dst = append(dst, dd.hist[dd.wrPos:]...)
		return append(dst, dd.hist[:dd.wrPos]...)
	}

	return append(dst, dd.hist[:dd.wrPos]...)
}

// readFlush returns a slice of the historical buffer that is ready to be
// emitted to the user. The data returned by readFlush must be fully consumed
// before calling any other dictDecoder methods.
func (dd *dictDecoder) readFlush() []byte {
	toRead := dd.hist[dd.rdPos:dd.wrPos]
	dd.flushed += int64(len(toRead))

	dd.rdPos = dd.wrPos
	if dd.wrPos == len(dd.hist) {
		dd.wrPos, dd.rdPos = 0, 0
		dd.full = true
	}

	return toRead
}

// decoded reports the total number of bytes written into the dictionary since
// init (i.e. excluding any preset dict bytes).
func (dd *dictDecoder) decoded() int64 {
	return dd.flushed + int64(dd.wrPos-dd.rdPos)
}
