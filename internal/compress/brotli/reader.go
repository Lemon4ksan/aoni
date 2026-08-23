// Copyright 2013 Google Inc. All Rights Reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli

import (
	"errors"
	"io"

	"github.com/lemon4ksan/foundation/silicon/pool"
)

type decodeError int

func (err decodeError) Error() string {
	return "brotli: " + string(decoderErrorString(int(err)))
}

var (
	errExcessiveInput = errors.New("brotli: excessive input")
	errInvalidState   = errors.New("brotli: invalid state")

	// ErrDecompressionBomb is returned by Reader.Read when decompressed stream
	// exceeds the configured maximum size limit (configured via SetMaxOutputSize).
	ErrDecompressionBomb = errors.New(
		"brotli: maximum decompressed output limit exceeded (decompression bomb detected)",
	)
)

// SetMaxOutputSize configures a maximum allowed decompressed size (in bytes)
// to defend against decompression bombs (zip bombs). If total decompressed bytes
// exceed this limit, Read returns ErrDecompressionBomb.
// Pass <= 0 to disable the limit (default).
func (r *Reader) SetMaxOutputSize(maxBytes int64) {
	r.maxOutputSize = maxBytes
}

// readBufSize is a "good" buffer size that avoids excessive round-trips
// between C and Go but doesn't waste too much memory on buffering.
// It is arbitrarily chosen to be equal to the constant used in io.Copy.
const readBufSize = 32 * 1024

var readerStorage = pool.NewPerPStorage(func() *Reader {
	return new(Reader)
})

// AcquireReader borrows a CPU-core local pooled [*Reader] bound to src with zero GC eviction jitter.
func AcquireReader(src io.Reader) *Reader {
	r := readerStorage.Get()
	_ = r.Reset(src)
	return r
}

// ReleaseReader resets and returns r back to the CPU-core local storage.
func ReleaseReader(r *Reader) {
	if r != nil {
		_ = r.Reset(nil)
		readerStorage.Put(r)
	}
}

// NewReader creates a new Reader reading the given reader.
func NewReader(src io.Reader) *Reader {
	r := new(Reader)
	_ = r.Reset(src)
	return r
}

// Close closes the Reader and implements io.ReadCloser.
func (r *Reader) Close() error {
	r.src = nil
	r.in = nil
	return nil
}

// Decompress decompresses a Brotli payload (RFC 7932) from src into dst.
func Decompress(dst, src []byte) ([]byte, error) {
	if len(src) == 0 {
		return dst[:0], nil
	}

	r := AcquireReader(nil)
	defer ReleaseReader(r)

	r.in = src

	if cap(dst) == 0 {
		dst = make([]byte, 0, len(src)*2)
	} else {
		dst = dst[:0]
	}

	for {
		if len(dst) == cap(dst) {
			newCap := max(cap(dst)*2, 1024)
			newDst := make([]byte, len(dst), newCap)
			copy(newDst, dst)
			dst = newDst
		}

		outCap := cap(dst) - len(dst)
		r.streamAvailIn = uint(len(r.in))
		r.streamAvailOut = uint(outCap)
		r.streamNextOut = dst[len(dst):cap(dst)]

		result := r.decompressStream(&r.streamAvailIn, &r.in, &r.streamAvailOut, &r.streamNextOut)
		written := outCap - int(r.streamAvailOut)
		dst = dst[:len(dst)+written]

		switch result {
		case decoderResultSuccess:
			if len(r.in) > 0 {
				return dst, errExcessiveInput
			}

			return dst, nil

		case decoderResultError:
			return dst, decodeError(r.getErrorCode())

		case decoderResultNeedsMoreOutput:
			continue

		case decoderNeedsMoreInput:
			if len(r.in) == 0 {
				return dst, io.ErrUnexpectedEOF
			}
		}
	}
}

// Reset discards the Reader's state and makes it equivalent to the result of
// its original state from NewReader, but reading from src instead.
// This permits reusing a Reader rather than allocating a new one.
// Error is always nil
func (r *Reader) Reset(src io.Reader) error {
	if r.errorCode < 0 {
		// There was an unrecoverable error, leaving the Reader's state
		// undefined. Clear out everything but the buffers.
		*r = Reader{
			buf:            r.buf,
			ringbuffer:     r.ringbuffer,
			contextModes:   r.contextModes,
			contextMap:     r.contextMap,
			distContextMap: r.distContextMap,
			blockTypeTrees: r.blockTypeTrees,
			literalHgroup: huffmanTreeGroup{
				htrees: r.literalHgroup.htrees,
				codes:  r.literalHgroup.codes,
			},
			distanceHgroup: huffmanTreeGroup{
				htrees: r.distanceHgroup.htrees,
				codes:  r.distanceHgroup.codes,
			},
			insertCopyHgroup: huffmanTreeGroup{
				htrees: r.insertCopyHgroup.htrees,
				codes:  r.insertCopyHgroup.codes,
			},
		}
	}

	r.initState()

	r.src = src
	if r.buf == nil {
		r.buf = make([]byte, readBufSize)
	}

	return nil
}

func (r *Reader) checkOutputLimit(n int) (int, error) {
	if r.maxOutputSize > 0 && r.totalOutput+int64(n) > r.maxOutputSize {
		allowed := int(r.maxOutputSize - r.totalOutput)
		if allowed < 0 {
			allowed = 0
		}

		r.totalOutput = r.maxOutputSize

		return allowed, ErrDecompressionBomb
	}

	r.totalOutput += int64(n)

	return n, nil
}

func (r *Reader) Read(p []byte) (n int, err error) {
	if !r.hasMoreOutput() && len(r.in) == 0 {
		m, readErr := r.src.Read(r.buf)
		if m == 0 {
			if readErr == io.EOF && r.state != stateDone {
				readErr = io.ErrUnexpectedEOF
			}

			// If readErr is `nil`, we just proxy underlying stream behavior.
			return 0, readErr
		}

		r.in = r.buf[:m]
	}

	if len(p) == 0 {
		return 0, nil
	}

	for {
		var written uint

		inLen := uint(len(r.in))
		outLen := uint(len(p))
		r.streamAvailIn = inLen
		r.streamAvailOut = outLen
		r.streamNextOut = p
		result := r.decompressStream(&r.streamAvailIn, &r.in, &r.streamAvailOut, &r.streamNextOut)
		written = outLen - r.streamAvailOut
		n = int(written)

		switch result {
		case decoderResultSuccess:
			if n > 0 {
				var limitErr error

				n, limitErr = r.checkOutputLimit(n)
				if limitErr != nil {
					return n, limitErr
				}
			}

			if len(r.in) > 0 {
				return n, errExcessiveInput
			}

			return n, nil

		case decoderResultError:
			return n, decodeError(r.getErrorCode())
		case decoderResultNeedsMoreOutput:
			if n == 0 {
				return 0, io.ErrShortBuffer
			}

			var limitErr error

			n, limitErr = r.checkOutputLimit(n)
			if limitErr != nil {
				return n, limitErr
			}

			return n, nil

		case decoderNeedsMoreInput:
		}

		if len(r.in) != 0 {
			return 0, errInvalidState
		}

		// Calling r.src.Read may block. Don't block if we have data to return.
		if n > 0 {
			var limitErr error

			n, limitErr = r.checkOutputLimit(n)
			if limitErr != nil {
				return n, limitErr
			}

			return n, nil
		}

		// Top off the buffer.
		encN, err := r.src.Read(r.buf)
		if encN == 0 {
			// Not enough data to complete decoding.
			if err == io.EOF {
				return 0, io.ErrUnexpectedEOF
			}

			return 0, err
		}

		r.in = r.buf[:encN]
	}
}
