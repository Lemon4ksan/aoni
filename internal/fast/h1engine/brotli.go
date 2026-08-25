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
	n := len(src)
	if n == 0 {
		return append(dst, 0x06)
	}

	l := uint32(n - 1)

	switch {
	case n <= 65536:
		// WBITS=24 (0x0F), ISLAST=0, MNIBBLES=00, MLEN (16 bits), ISUNCOMPRESSED=1
		b0 := byte(0x0F | ((l & 1) << 7))
		b1 := byte(l >> 1)
		b2 := byte((l >> 9) | 0x80)

		dst = append(dst, b0, b1, b2)
		dst = append(dst, src...)
		return append(dst, 0x03)

	case n <= 1048576:
		// WBITS=24 (0x0F), ISLAST=0, MNIBBLES=01 (0x20), MLEN (20 bits), ISUNCOMPRESSED=1 (0x08)
		b0 := byte(0x2F | ((l & 1) << 7))
		b1 := byte(l >> 1)
		b2 := byte(l >> 9)
		b3 := byte((l >> 17) | 0x08)

		dst = append(dst, b0, b1, b2, b3)
		dst = append(dst, src...)
		return append(dst, 0x03)

	default:
		// WBITS=24 (0x0F), ISLAST=0, MNIBBLES=10 (0x40), MLEN (24 bits), ISUNCOMPRESSED=1 (0x80)
		b0 := byte(0x4F | ((l & 1) << 7))
		b1 := byte(l >> 1)
		b2 := byte(l >> 9)
		b3 := byte((l >> 17) | 0x80)

		dst = append(dst, b0, b1, b2, b3)
		dst = append(dst, src...)
		return append(dst, 0x03)
	}
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
