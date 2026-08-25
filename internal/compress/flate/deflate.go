// Copyright 2009 The Go Authors. All rights reserved.
// Copyright (c) 2015-2023 Klaus Post. All rights reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flate

import (
	"fmt"
	"io"
)

const (
	NoCompression      = 0
	BestSpeed          = 1
	BestCompression    = 9
	DefaultCompression = -1

	// HuffmanOnly disables Lempel-Ziv match searching and only performs Huffman
	// entropy encoding for already compressed streams.
	HuffmanOnly         = -2
	ConstantCompression = HuffmanOnly // compatibility alias.

	maxStoreBlockSize = 65535
	debugDeflate      = false
)

type compressor struct {
	w *huffmanBitWriter

	// compression algorithm
	fill func(*compressor, []byte) int // copy data to window
	step func(*compressor)             // process window

	window    []byte
	windowEnd int
	err       error

	// queued output tokens
	tokens tokens
	fast   fastEnc

	sync  bool // requesting flush
	level int
}

func (d *compressor) store() {
	if d.windowEnd > 0 && (d.windowEnd == maxStoreBlockSize || d.sync) {
		d.err = d.writeStoredBlock(d.window[:d.windowEnd])
		d.windowEnd = 0
	}
}

// fillBlock fills the buffer with data for fast/huffman-only compression.
func (d *compressor) fillBlock(b []byte) int {
	n := copy(d.window[d.windowEnd:], b)
	d.windowEnd += n
	return n
}

// storeHuff compresses and stores currently added data using Huffman-only encoding.
func (d *compressor) storeHuff() {
	if d.windowEnd < len(d.window) && !d.sync || d.windowEnd == 0 {
		return
	}

	d.w.writeBlockHuff(false, d.window[:d.windowEnd], d.sync)
	d.err = d.w.err
	d.windowEnd = 0
}

// storeFast compresses accumulated block data using the table-driven fast encoder.
func (d *compressor) storeFast() {
	if d.windowEnd < len(d.window) {
		if !d.sync {
			return
		}

		// Handle extremely small sizes.
		if d.windowEnd < 128 {
			if d.windowEnd == 0 {
				return
			}

			if d.windowEnd <= 32 {
				d.err = d.writeStoredBlock(d.window[:d.windowEnd])
			} else {
				d.w.writeBlockHuff(false, d.window[:d.windowEnd], true)
				d.err = d.w.err
			}

			d.tokens.Reset()
			d.windowEnd = 0
			d.fast.Reset()

			return
		}
	}

	d.fast.Encode(&d.tokens, d.window[:d.windowEnd])
	// If we made zero matches, store the block as is.
	if d.tokens.n == 0 {
		d.err = d.writeStoredBlock(d.window[:d.windowEnd])
		// If we removed less than 1/16th, huffman compress the block.
	} else if int(d.tokens.n) > d.windowEnd-(d.windowEnd>>4) {
		d.w.writeBlockHuff(false, d.window[:d.windowEnd], d.sync)
		d.err = d.w.err
	} else {
		d.w.writeBlockDynamic(&d.tokens, false, d.window[:d.windowEnd], d.sync)
		d.err = d.w.err
	}

	d.tokens.Reset()
	d.windowEnd = 0
}

func (d *compressor) writeStoredBlock(buf []byte) error {
	if d.w.writeStoredHeader(len(buf), false); d.w.err != nil {
		return d.w.err
	}

	d.w.writeBytes(buf)

	return d.w.err
}

// write will add input byte to the stream.
func (d *compressor) write(b []byte) (n int, err error) {
	if d.err != nil {
		return 0, d.err
	}

	n = len(b)
	for len(b) > 0 {
		if d.windowEnd == len(d.window) || d.sync {
			d.step(d)
		}

		b = b[d.fill(d, b):]
		if d.err != nil {
			return 0, d.err
		}
	}

	return n, d.err
}

func (d *compressor) syncFlush() error {
	d.sync = true
	if d.err != nil {
		return d.err
	}

	d.step(d)

	if d.err == nil {
		d.w.writeStoredHeader(0, false)
		d.w.flush()
		d.err = d.w.err
	}

	d.sync = false

	return d.err
}

func (d *compressor) init(w io.Writer, level int) (err error) {
	d.w = newHuffmanBitWriter(w)

	switch {
	case level == NoCompression:
		d.window = make([]byte, maxStoreBlockSize)
		d.fill = (*compressor).fillBlock
		d.step = (*compressor).store
	case level == ConstantCompression:
		d.w.logNewTablePenalty = 10
		d.window = make([]byte, 32<<10)
		d.fill = (*compressor).fillBlock
		d.step = (*compressor).storeHuff
	case level == DefaultCompression, (level >= 1 && level <= 9):
		d.w.logNewTablePenalty = 7
		d.fast = newFastEnc(level)
		d.window = make([]byte, maxStoreBlockSize)
		d.fill = (*compressor).fillBlock
		d.step = (*compressor).storeFast
	default:
		return fmt.Errorf("flate: invalid compression level %d: want value in range [-2, 9]", level)
	}

	d.level = level

	return nil
}

// reset the state of the compressor.
func (d *compressor) reset(w io.Writer) {
	d.w.reset(w)
	d.sync = false
	d.err = nil
	if d.fast != nil {
		d.fast.Reset()
		d.windowEnd = 0
		d.tokens.Reset()

		return
	}

	d.windowEnd = 0
}

func (d *compressor) close() error {
	if d.err != nil {
		return d.err
	}

	d.sync = true
	d.step(d)

	if d.err != nil {
		return d.err
	}

	if d.w.writeStoredHeader(0, true); d.w.err != nil {
		return d.w.err
	}

	d.w.flush()
	d.w.reset(nil)

	return d.w.err
}

// Writer takes data written to it and writes the compressed form of that data to an underlying writer.
type Writer struct {
	d compressor
}

// NewWriter returns a new Writer compressing data at the given level.
// Following zlib, levels range from 1 (BestSpeed) to 9 (BestCompression).
func NewWriter(w io.Writer, level int) (*Writer, error) {
	var dw Writer
	if err := dw.d.init(w, level); err != nil {
		return nil, err
	}

	return &dw, nil
}

// Write writes data to w, which will eventually write the compressed form of data to its underlying writer.
func (w *Writer) Write(data []byte) (n int, err error) {
	return w.d.write(data)
}

// Flush flushes any pending data to the underlying writer.
func (w *Writer) Flush() error {
	return w.d.syncFlush()
}

// Close flushes and closes the writer.
func (w *Writer) Close() error {
	return w.d.close()
}

// Reset discards the writer's state and makes it equivalent to NewWriter called with dst and w's level.
func (w *Writer) Reset(dst io.Writer) {
	w.d.reset(dst)
}
