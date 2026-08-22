// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package compress provides a trimmed, zero-allocation multi-algorithm decompression engine
// supporting RFC 1952 (gzip), RFC 7932 (brotli), RFC 8878 (zstd), and RFC 1951 (deflate).
package compress

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni/internal/compress/brotli"
	"github.com/lemon4ksan/aoni/internal/compress/flate"
	"github.com/lemon4ksan/aoni/internal/compress/gzip"
	"github.com/lemon4ksan/aoni/internal/compress/zstd"
)

var (
	// ErrUnsupportedEncoding is returned when a Content-Encoding algorithm is unknown.
	ErrUnsupportedEncoding = errors.New("compress: unsupported content encoding")

	// ErrDecompressionFailed is returned when decompression payload is malformed.
	ErrDecompressionFailed = errors.New("compress: decompression failed")
)

var (
	zstdDecoderPool = sync.Pool{
		New: func() any {
			dec, _ := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true))
			return dec
		},
	}

	gzipReaderPool = sync.Pool{
		New: func() any {
			return new(gzip.Reader)
		},
	}
)

// Decompress decodes compressed src into dst using the specified Content-Encoding algorithm.
// Supports "gzip", "br", "zstd", and "deflate".
func Decompress(encoding string, src, dst []byte) ([]byte, error) {
	if len(src) == 0 {
		return nil, nil
	}

	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "gzip", "x-gzip":
		return Gunzip(src, dst)
	case "br":
		return Unbrotli(src, dst)
	case "zstd":
		return Unzstd(src, dst)
	case "deflate":
		return Inflate(src, dst)
	default:
		return nil, ErrUnsupportedEncoding
	}
}

// Gunzip decompresses a gzip payload (RFC 1952) from src into dst.
func Gunzip(src, dst []byte) ([]byte, error) {
	zr := gzipReaderPool.Get().(*gzip.Reader)
	defer gzipReaderPool.Put(zr)

	if err := zr.Reset(bytes.NewReader(src)); err != nil {
		return nil, err
	}

	defer zr.Close()

	if dst == nil {
		dst = make([]byte, 0, len(src)*2)
	}

	buf := bytes.NewBuffer(dst[:0])
	if _, err := io.Copy(buf, zr); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Unbrotli decompresses a Brotli payload (RFC 7932) from src into dst.
func Unbrotli(src, dst []byte) ([]byte, error) {
	return brotli.Decompress(dst, src)
}

// Unzstd decompresses a Zstandard payload (RFC 8878) from src into dst with zero allocations.
func Unzstd(src, dst []byte) ([]byte, error) {
	dec := zstdDecoderPool.Get().(*zstd.Decoder)
	defer zstdDecoderPool.Put(dec)

	return dec.DecodeAll(src, dst)
}

// Inflate decompresses raw deflate payload (RFC 1951) from src into dst.
func Inflate(src, dst []byte) ([]byte, error) {
	fr := flate.NewReader(bytes.NewReader(src))
	defer fr.Close()

	if dst == nil {
		dst = make([]byte, 0, len(src)*2)
	}

	buf := bytes.NewBuffer(dst[:0])
	if _, err := io.Copy(buf, fr); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// AcquireZstdReader borrows a pooled [*zstd.Decoder] bound to r.
func AcquireZstdReader(r io.Reader) (*zstd.Decoder, error) {
	dec := zstdDecoderPool.Get().(*zstd.Decoder)
	if err := dec.Reset(r); err != nil {
		zstdDecoderPool.Put(dec)
		return nil, err
	}

	return dec, nil
}

// ReleaseZstdReader returns dec back to the pool.
func ReleaseZstdReader(dec *zstd.Decoder) {
	if dec != nil {
		_ = dec.Reset(nil)
		zstdDecoderPool.Put(dec)
	}
}

// AcquireGzipReader borrows a pooled [*gzip.Reader] bound to r.
func AcquireGzipReader(r io.Reader) (*gzip.Reader, error) {
	zr := gzipReaderPool.Get().(*gzip.Reader)
	if err := zr.Reset(r); err != nil {
		gzipReaderPool.Put(zr)
		return nil, err
	}

	return zr, nil
}

// ReleaseGzipReader returns zr back to the pool.
func ReleaseGzipReader(zr *gzip.Reader) {
	if zr != nil {
		_ = zr.Close()
		gzipReaderPool.Put(zr)
	}
}

// AcquireBrotliReader borrows a pooled [*brotli.Reader] bound to r.
func AcquireBrotliReader(r io.Reader) (*brotli.Reader, error) {
	return brotli.AcquireReader(r), nil
}

// ReleaseBrotliReader returns br back to the pool.
func ReleaseBrotliReader(br *brotli.Reader) {
	brotli.ReleaseReader(br)
}

// IsCompressed reports whether b contains compression magic bytes for gzip, brotli, or zstd.
func IsCompressed(b []byte) bool {
	if len(b) < 4 {
		return false
	}

	// Gzip magic (0x1f, 0x8b)
	if b[0] == 0x1f && b[1] == 0x8b {
		return true
	}

	// Zstd magic (0x28, 0xb5, 0x2f, 0xfd)
	if b[0] == 0x28 && b[1] == 0xb5 && b[2] == 0x2f && b[3] == 0xfd {
		return true
	}

	return false
}

// MatchesEncoding reports whether encoding matches the specified algorithm (case-insensitive).
func MatchesEncoding(headerEncoding []byte, algorithm string) bool {
	return bytesconv.ContainsFoldASCII(headerEncoding, algorithm)
}
