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

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/pool"

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
	zstdDecoderStorage = pool.NewPerPStorage(func() *zstd.Decoder {
		dec, _ := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true))
		return dec
	})

	gzipReaderStorage = pool.NewPerPStorage(func() *gzip.Reader {
		return new(gzip.Reader)
	})

	flateReaderStorage = pool.NewPerPStorage(func() flate.Resetter {
		return flate.NewReader(nil).(flate.Resetter)
	})

	bytesReaderStorage = pool.NewPerPStorage(func() *bytes.Reader {
		return bytes.NewReader(nil)
	})
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
	zr := gzipReaderStorage.Get()
	defer gzipReaderStorage.Put(zr)

	br := bytesReaderStorage.Get()
	defer bytesReaderStorage.Put(br)

	br.Reset(src)

	if err := zr.Reset(br); err != nil {
		return nil, err
	}

	defer zr.Close()

	return readAllSlice(zr, src, dst)
}

// Unbrotli decompresses a Brotli payload (RFC 7932) from src into dst.
func Unbrotli(src, dst []byte) ([]byte, error) {
	return brotli.Decompress(dst, src)
}

// Unzstd decompresses a Zstandard payload (RFC 8878) from src into dst with zero allocations.
func Unzstd(src, dst []byte) ([]byte, error) {
	dec := zstdDecoderStorage.Get()
	defer zstdDecoderStorage.Put(dec)

	return dec.DecodeAll(src, dst)
}

// Inflate decompresses raw deflate payload (RFC 1951) from src into dst.
func Inflate(src, dst []byte) ([]byte, error) {
	fr := flateReaderStorage.Get()
	defer flateReaderStorage.Put(fr)

	br := bytesReaderStorage.Get()
	defer bytesReaderStorage.Put(br)

	br.Reset(src)

	if err := fr.Reset(br, nil); err != nil {
		return nil, err
	}

	return readAllSlice(fr.(io.Reader), src, dst)
}

func readAllSlice(r io.Reader, src, dst []byte) ([]byte, error) {
	if dst == nil {
		dst = make([]byte, 0, len(src)*2)
	} else {
		dst = dst[:0]
	}

	for {
		if len(dst) == cap(dst) {
			newCap := cap(dst) * 2
			if newCap == 0 {
				newCap = 1024
			}

			newDst := make([]byte, len(dst), newCap)
			copy(newDst, dst)
			dst = newDst
		}

		n, err := r.Read(dst[len(dst):cap(dst)])
		dst = dst[:len(dst)+n]

		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, err
		}
	}

	return dst, nil
}

// AcquireZstdReader borrows a pooled [*zstd.Decoder] bound to r.
func AcquireZstdReader(r io.Reader) (*zstd.Decoder, error) {
	dec := zstdDecoderStorage.Get()
	if err := dec.Reset(r); err != nil {
		zstdDecoderStorage.Put(dec)
		return nil, err
	}

	return dec, nil
}

// ReleaseZstdReader returns dec back to the pool.
func ReleaseZstdReader(dec *zstd.Decoder) {
	if dec != nil {
		_ = dec.Reset(nil)
		zstdDecoderStorage.Put(dec)
	}
}

// AcquireGzipReader borrows a pooled [*gzip.Reader] bound to r.
func AcquireGzipReader(r io.Reader) (*gzip.Reader, error) {
	zr := gzipReaderStorage.Get()
	if err := zr.Reset(r); err != nil {
		gzipReaderStorage.Put(zr)
		return nil, err
	}

	return zr, nil
}

// ReleaseGzipReader returns zr back to the pool.
func ReleaseGzipReader(zr *gzip.Reader) {
	if zr != nil {
		_ = zr.Close()
		gzipReaderStorage.Put(zr)
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

// NewReader returns a pooled [io.ReadCloser] that decompresses data from r
// using the specified Content-Encoding algorithm ("gzip", "br", "zstd", "deflate").
// When Close is called, the underlying decompressor and buffers are automatically
// returned to their pools, and r.Close() is called if r implements io.Closer.
func NewReader(encoding string, r io.Reader) (io.ReadCloser, error) {
	if r == nil {
		return nil, errors.New("compress: nil reader")
	}

	enc := strings.ToLower(strings.TrimSpace(encoding))
	switch enc {
	case "gzip", "x-gzip":
		zr := gzipReaderStorage.Get()
		if err := zr.Reset(r); err != nil {
			gzipReaderStorage.Put(zr)

			return nil, err
		}

		return &decompressReadCloser{
			reader: zr,
			closer: closerOf(r),
			release: func() {
				_ = zr.Close()
				gzipReaderStorage.Put(zr)
			},
		}, nil

	case "br":
		br := brotli.AcquireReader(r)

		return &decompressReadCloser{
			reader: br,
			closer: closerOf(r),
			release: func() {
				brotli.ReleaseReader(br)
			},
		}, nil

	case "zstd":
		dec := zstdDecoderStorage.Get()
		if err := dec.Reset(r); err != nil {
			zstdDecoderStorage.Put(dec)

			return nil, err
		}

		return &decompressReadCloser{
			reader: dec,
			closer: closerOf(r),
			release: func() {
				_ = dec.Reset(nil)
				zstdDecoderStorage.Put(dec)
			},
		}, nil

	case "deflate":
		fr := flateReaderStorage.Get()
		if err := fr.Reset(r, nil); err != nil {
			flateReaderStorage.Put(fr)

			return nil, err
		}

		return &decompressReadCloser{
			reader: fr.(io.Reader),
			closer: closerOf(r),
			release: func() {
				flateReaderStorage.Put(fr)
			},
		}, nil

	case "", "identity":
		if rc, ok := r.(io.ReadCloser); ok {
			return rc, nil
		}

		return io.NopCloser(r), nil

	default:
		return nil, ErrUnsupportedEncoding
	}
}

type decompressReadCloser struct {
	reader  io.Reader
	closer  io.Closer
	release func()
}

func (d *decompressReadCloser) Read(p []byte) (int, error) {
	return d.reader.Read(p)
}

func (d *decompressReadCloser) Close() error {
	var err error
	if d.closer != nil {
		err = d.closer.Close()
	}

	if d.release != nil {
		d.release()
		d.release = nil
	}

	return err
}

func closerOf(r io.Reader) io.Closer {
	if c, ok := r.(io.Closer); ok {
		return c
	}

	return nil
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
