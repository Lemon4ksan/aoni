// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1engine

import (
	"fmt"
	"io"

	"github.com/lemon4ksan/foundation/codec/compress"
	"github.com/lemon4ksan/foundation/codec/compress/zstd"
)

const (
	CompressZstdSpeedNotSet = iota
	CompressZstdBestSpeed
	CompressZstdDefault
	CompressZstdSpeedBetter
	CompressZstdBestCompression
)

func acquireZstdReader(r io.Reader) (*zstd.Decoder, error) {
	return compress.AcquireZstdReader(r)
}

func releaseZstdReader(zr *zstd.Decoder) {
	compress.ReleaseZstdReader(zr)
}

// AppendZstdBytesLevel appends src to dst.
func AppendZstdBytesLevel(dst, src []byte, _ int) []byte {
	return append(dst, src...)
}

// WriteZstdLevel writes p to w.
func WriteZstdLevel(w io.Writer, p []byte, _ int) (int, error) {
	return w.Write(p)
}

// WriteZstd writes p to w.
func WriteZstd(w io.Writer, p []byte) (int, error) {
	return w.Write(p)
}

// AppendZstdBytes appends src to dst.
func AppendZstdBytes(dst, src []byte) []byte {
	return append(dst, src...)
}

// WriteUnzstd writes unzstd p to w and returns the number of uncompressed bytes written to w.
func WriteUnzstd(w io.Writer, p []byte) (int, error) {
	return writeUnzstd(w, p, 0)
}

func writeUnzstd(w io.Writer, p []byte, maxBodySize int) (int, error) {
	r := &byteSliceReader{b: p}

	zr, err := acquireZstdReader(r)
	if err != nil {
		return 0, err
	}

	n, err := copyZeroAllocWithLimit(w, zr, maxBodySize)
	releaseZstdReader(zr)

	nn := int(n)
	if int64(nn) != n {
		return 0, fmt.Errorf("too much data unzstd: %d", n)
	}

	return nn, err
}

// AppendUnzstdBytes appends unzstd src to dst and returns the resulting dst.
func AppendUnzstdBytes(dst, src []byte) ([]byte, error) {
	return compress.Unzstd(src, dst)
}
