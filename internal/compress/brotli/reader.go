// Copyright 2013 Google Inc. All Rights Reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli

import (
	"errors"
	"io"
)

type decodeError int

func (err decodeError) Error() string {
	return "brotli: " + string(decoderErrorString(int(err)))
}

var (
	errExcessiveInput = errors.New("brotli: excessive input")
	errInvalidState   = errors.New("brotli: invalid state")
)

// readBufSize is a "good" buffer size that avoids excessive round-trips
// between C and Go but doesn't waste too much memory on buffering.
// It is arbitrarily chosen to be equal to the constant used in io.Copy.
const readBufSize = 32 * 1024

// NewReader creates a new Reader reading the given reader.
func NewReader(src io.Reader) *Reader {
	r := new(Reader)
	_ = r.Reset(src)
	return r
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
		inRemaining := inLen
		outRemaining := outLen
		result := r.decompressStream(&inRemaining, &r.in, &outRemaining, &p)
		written = outLen - outRemaining
		n = int(written)

		switch result {
		case decoderResultSuccess:
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

			return n, nil

		case decoderNeedsMoreInput:
		}

		if len(r.in) != 0 {
			return 0, errInvalidState
		}

		// Calling r.src.Read may block. Don't block if we have data to return.
		if n > 0 {
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
