// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1engine

import (
	"fmt"
	"io"

	"github.com/lemon4ksan/aoni/internal/compress/brotli"
)

// Supported compression levels.
const (
	CompressBrotliNoCompression      = 0
	CompressBrotliBestSpeed          = 1
	CompressBrotliBestCompression    = 11
	CompressBrotliDefaultCompression = 4
)

func acquireBrotliReader(r io.Reader) (*brotli.Reader, error) {
	return brotli.AcquireReader(r), nil
}

func releaseBrotliReader(zr *brotli.Reader) {
	brotli.ReleaseReader(zr)
}

// AppendBrotliBytesLevel appends brotlied src to dst using valid RFC 7932 uncompressed meta-blocks.
func AppendBrotliBytesLevel(dst, src []byte, _ int) []byte {
	if len(src) == 0 {
		return append(dst, 0x06)
	}

	for len(src) > 0 {
		chunkLen := len(src)
		if chunkLen > 65536 {
			chunkLen = 65536
		}

		chunk := src[:chunkLen]
		src = src[chunkLen:]

		header := (uint32(chunkLen-1) << 4) | (1 << 20)
		b0 := byte(header & 0xFF)
		b1 := byte((header >> 8) & 0xFF)
		b2 := byte((header >> 16) & 0xFF)

		dst = append(dst, b0, b1, b2)
		dst = append(dst, chunk...)
	}

	dst = append(dst, 0x03)
	return dst
}

// WriteBrotliLevel writes p to w.
func WriteBrotliLevel(w io.Writer, p []byte, level int) (int, error) {
	b := AppendBrotliBytesLevel(nil, p, level)
	return w.Write(b)
}

// WriteBrotli writes p to w.
func WriteBrotli(w io.Writer, p []byte) (int, error) {
	return WriteBrotliLevel(w, p, CompressBrotliDefaultCompression)
}

// AppendBrotliBytes appends src to dst.
func AppendBrotliBytes(dst, src []byte) []byte {
	return AppendBrotliBytesLevel(dst, src, CompressBrotliDefaultCompression)
}

// WriteUnbrotli writes unbrotlied p to w and returns the number of uncompressed bytes written to w.
func WriteUnbrotli(w io.Writer, p []byte) (int, error) {
	return writeUnbrotli(w, p, 0)
}

func writeUnbrotli(w io.Writer, p []byte, maxBodySize int) (int, error) {
	r := &byteSliceReader{b: p}

	zr, err := acquireBrotliReader(r)
	if err != nil {
		return 0, err
	}

	n, err := copyZeroAllocWithLimit(w, zr, maxBodySize)
	releaseBrotliReader(zr)

	nn := int(n)
	if int64(nn) != n {
		return 0, fmt.Errorf("too much data unbrotlied: %d", n)
	}

	return nn, err
}

// AppendUnbrotliBytes appends unbrotlied src to dst and returns the resulting dst.
func AppendUnbrotliBytes(dst, src []byte) ([]byte, error) {
	return brotli.Decompress(dst, src)
}
