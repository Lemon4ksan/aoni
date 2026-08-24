// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package compress provides a trimmed, zero-allocation multi-algorithm decompression engine
// supporting RFC 1952 (gzip), RFC 7932 (brotli), RFC 8878 (zstd), and RFC 1951 (deflate).
package compress

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/lemon4ksan/foundation/borrow"
	"github.com/lemon4ksan/foundation/silicon/pool"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni/internal/compress/brotli"
	"github.com/lemon4ksan/aoni/internal/compress/flate"
	"github.com/lemon4ksan/aoni/internal/compress/gzip"
	"github.com/lemon4ksan/aoni/internal/compress/zstd"
)

const (
	// DefaultMaxDecompressedSize is the default maximum permitted output size (100 MB).
	DefaultMaxDecompressedSize = 100 * 1024 * 1024

	// MaxAmplificationRatio defines the maximum decompression expansion ratio (250x).
	MaxAmplificationRatio = 250
)

var (
	// ErrUnsupportedEncoding is returned when a Content-Encoding algorithm is unknown.
	ErrUnsupportedEncoding = errors.New("compress: unsupported content encoding")

	// ErrDecompressionFailed is returned when decompression payload is malformed.
	ErrDecompressionFailed = errors.New("compress: decompression failed")

	// ErrDecompressionBomb is returned when decompressed payload exceeds size/amplification limits.
	ErrDecompressionBomb = errors.New(
		"compress: maximum decompressed output limit exceeded (decompression bomb detected)",
	)
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

	byteBufferStorage = pool.NewPerPStorage(func() *bytes.Buffer {
		return bytes.NewBuffer(make([]byte, 0, 4096))
	})

	gzipWriterStorage = pool.NewPerPStorage(func() *gzip.Writer {
		return gzip.NewWriter(io.Discard)
	})

	flateWriterStorage = pool.NewPerPStorage(func() *flate.Writer {
		w, _ := flate.NewWriter(io.Discard, flate.DefaultCompression)
		return w
	})
)

// Decompress decodes compressed src into dst using the specified Content-Encoding algorithm.
// Supports "gzip", "br", "zstd", and "deflate".
func Decompress(encoding string, src, dst []byte) ([]byte, error) {
	if len(src) == 0 {
		return nil, nil
	}

	// Fast-path for canonical lowercase encodings (zero allocation)
	switch encoding {
	case "gzip", "x-gzip":
		return Gunzip(src, dst)
	case "br":
		return Unbrotli(src, dst)
	case "zstd":
		return Unzstd(src, dst)
	case "deflate":
		return Inflate(src, dst)
	case "identity", "":
		if cap(dst) < len(src) {
			dst = make([]byte, len(src))
		} else {
			dst = dst[:len(src)]
		}

		copy(dst, src)

		return dst, nil
	}

	// Fallback for mixed-case or padded strings
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "gzip", "x-gzip":
		return Gunzip(src, dst)
	case "br":
		return Unbrotli(src, dst)
	case "zstd":
		return Unzstd(src, dst)
	case "deflate":
		return Inflate(src, dst)
	case "identity", "":
		if cap(dst) < len(src) {
			dst = make([]byte, len(src))
		} else {
			dst = dst[:len(src)]
		}

		copy(dst, src)

		return dst, nil

	default:
		return nil, ErrUnsupportedEncoding
	}
}

// DecompressScoped decodes compressed src directly into a zero-allocation scoped buffer
// bound to the lifetime of s.
func DecompressScoped(s *borrow.Scope, encoding string, src []byte) (borrow.Bytes, error) {
	if len(src) == 0 {
		return borrow.Bytes{}, nil
	}

	// Fast-path for canonical lowercase encodings (zero allocation)
	switch encoding {
	case "gzip", "x-gzip":
		return GunzipScoped(s, src)
	case "br":
		return UnbrotliScoped(s, src)
	case "zstd":
		return UnzstdScoped(s, src)
	case "deflate":
		return InflateScoped(s, src)
	case "identity", "":
		if s == nil {
			return borrow.NewBytes(src, nil), nil
		}

		b := s.AllocBytes(len(src))
		copy(b.AsSlice(), src)

		return b, nil
	}

	// Fallback for mixed-case or padded strings
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "gzip", "x-gzip":
		return GunzipScoped(s, src)
	case "br":
		return UnbrotliScoped(s, src)
	case "zstd":
		return UnzstdScoped(s, src)
	case "deflate":
		return InflateScoped(s, src)
	case "identity", "":
		if s == nil {
			return borrow.NewBytes(src, nil), nil
		}

		b := s.AllocBytes(len(src))
		copy(b.AsSlice(), src)

		return b, nil

	default:
		return borrow.Bytes{}, ErrUnsupportedEncoding
	}
}

// gzipEstimatedSize reads RFC 1952 ISIZE footer when available to predict exact uncompressed size.
func gzipEstimatedSize(src []byte) int {
	if len(src) >= 18 && src[0] == 0x1f && src[1] == 0x8b {
		isize := int(binary.LittleEndian.Uint32(src[len(src)-4:]))
		if isize > 0 && isize <= 256*1024*1024 {
			return isize
		}
	}

	return max(len(src)*4, 4096)
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

	if cap(dst) == 0 {
		dst = make([]byte, 0, gzipEstimatedSize(src))
	} else {
		dst = dst[:0]
	}

	return readAllSlice(zr, src, dst)
}

// GunzipScoped decompresses a gzip payload directly into a zero-allocation scoped buffer.
func GunzipScoped(s *borrow.Scope, src []byte) (borrow.Bytes, error) {
	zr := gzipReaderStorage.Get()
	defer gzipReaderStorage.Put(zr)

	br := bytesReaderStorage.Get()
	defer bytesReaderStorage.Put(br)

	br.Reset(src)

	if err := zr.Reset(br); err != nil {
		return borrow.Bytes{}, err
	}

	defer zr.Close()

	return readAllSliceScoped(s, zr, gzipEstimatedSize(src))
}

// Unbrotli decompresses a Brotli payload (RFC 7932) from src into dst.
func Unbrotli(src, dst []byte) ([]byte, error) {
	return brotli.Decompress(dst, src)
}

// UnbrotliScoped decompresses a Brotli payload directly into a zero-allocation scoped buffer.
func UnbrotliScoped(s *borrow.Scope, src []byte) (borrow.Bytes, error) {
	return brotli.DecompressScoped(s, src)
}

// Unzstd decompresses a Zstandard payload (RFC 8878) from src into dst with zero allocations.
func Unzstd(src, dst []byte) ([]byte, error) {
	dec := zstdDecoderStorage.Get()
	defer zstdDecoderStorage.Put(dec)

	return dec.DecodeAll(src, dst)
}

// UnzstdScoped decompresses a Zstandard payload directly into a zero-allocation scoped buffer.
func UnzstdScoped(s *borrow.Scope, src []byte) (borrow.Bytes, error) {
	if len(src) == 0 {
		return borrow.Bytes{}, nil
	}

	if s == nil {
		raw, err := Unzstd(src, nil)
		if err != nil {
			return borrow.Bytes{}, err
		}

		return borrow.NewBytes(raw, nil), nil
	}

	dec := zstdDecoderStorage.Get()
	defer zstdDecoderStorage.Put(dec)

	initCap := max(len(src)*2, 4096)
	b := s.AllocBytes(initCap)
	dst := b.AsSlice()[:0]

	decompressed, err := dec.DecodeAll(src, dst)
	if err != nil {
		return borrow.Bytes{}, err
	}

	if len(decompressed) > cap(dst) {
		newB := s.AllocBytes(len(decompressed))
		copy(newB.AsSlice(), decompressed)

		return newB, nil
	}

	return b.Slice(0, len(decompressed)), nil
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

	if cap(dst) == 0 {
		dst = make([]byte, 0, max(len(src)*4, 4096))
	} else {
		dst = dst[:0]
	}

	return readAllSlice(fr.(io.Reader), src, dst)
}

// InflateScoped decompresses raw deflate payload directly into a zero-allocation scoped buffer.
func InflateScoped(s *borrow.Scope, src []byte) (borrow.Bytes, error) {
	fr := flateReaderStorage.Get()
	defer flateReaderStorage.Put(fr)

	br := bytesReaderStorage.Get()
	defer bytesReaderStorage.Put(br)

	br.Reset(src)

	if err := fr.Reset(br, nil); err != nil {
		return borrow.Bytes{}, err
	}

	return readAllSliceScoped(s, fr.(io.Reader), max(len(src)*4, 4096))
}

func readAllSlice(r io.Reader, src, dst []byte) ([]byte, error) {
	if cap(dst) == 0 {
		dst = make([]byte, 0, max(len(src)*4, 4096))
	} else {
		dst = dst[:0]
	}

	maxLimit := DefaultMaxDecompressedSize
	if len(src) > 0 {
		amplifiedLimit := len(src) * MaxAmplificationRatio
		if amplifiedLimit > 0 && amplifiedLimit < maxLimit {
			maxLimit = amplifiedLimit
		}
	}

	for {
		if len(dst) == cap(dst) {
			if len(dst) >= maxLimit {
				return nil, ErrDecompressionBomb
			}

			newCap := max(cap(dst)*2, 1024)
			if newCap > maxLimit {
				newCap = maxLimit + 1
			}

			newDst := make([]byte, len(dst), newCap)
			copy(newDst, dst)
			dst = newDst
		}

		n, err := r.Read(dst[len(dst):cap(dst)])
		dst = dst[:len(dst)+n]

		if len(dst) > maxLimit {
			return nil, ErrDecompressionBomb
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, err
		}
	}

	return dst, nil
}

func readAllSliceScoped(s *borrow.Scope, r io.Reader, estimatedCap int) (borrow.Bytes, error) {
	if s == nil {
		raw, err := readAllSlice(r, nil, make([]byte, 0, estimatedCap))
		if err != nil {
			return borrow.Bytes{}, err
		}

		return borrow.NewBytes(raw, nil), nil
	}

	initCap := max(estimatedCap, 1024)
	if initCap > DefaultMaxDecompressedSize {
		initCap = DefaultMaxDecompressedSize
	}

	b := s.AllocBytes(initCap)
	dst := b.AsSlice()[:0]

	for {
		if len(dst) == cap(dst) {
			if len(dst) >= DefaultMaxDecompressedSize {
				return borrow.Bytes{}, ErrDecompressionBomb
			}

			newCap := max(cap(dst)*2, 1024)
			if newCap > DefaultMaxDecompressedSize {
				newCap = DefaultMaxDecompressedSize + 1
			}

			newB := s.AllocBytes(newCap)
			newDst := newB.AsSlice()[:len(dst)]
			copy(newDst, dst)
			dst = newDst
			b = newB
		}

		n, err := r.Read(dst[len(dst):cap(dst)])
		dst = dst[:len(dst)+n]

		if len(dst) > DefaultMaxDecompressedSize {
			return borrow.Bytes{}, ErrDecompressionBomb
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return borrow.Bytes{}, err
		}
	}

	return b.Slice(0, len(dst)), nil
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
		br := bytesReaderStorage.Get()
		br.Reset(nil)

		if err := fr.Reset(r, nil); err != nil {
			flateReaderStorage.Put(fr)
			bytesReaderStorage.Put(br)

			return nil, err
		}

		return &decompressReadCloser{
			reader: fr.(io.Reader),
			closer: closerOf(r),
			release: func() {
				flateReaderStorage.Put(fr)
				bytesReaderStorage.Put(br)
			},
		}, nil

	case "identity", "":
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
	if d.release != nil {
		d.release()
		d.release = nil
	}

	if d.closer != nil {
		return d.closer.Close()
	}

	return nil
}

func closerOf(r io.Reader) io.Closer {
	if c, ok := r.(io.Closer); ok {
		return c
	}

	return nil
}

// IsCompressed checks magic header bytes to detect gzip or zstd compressed payloads.
func IsCompressed(data []byte) bool {
	if len(data) < 2 {
		return false
	}

	// Gzip magic: 0x1f, 0x8b
	if data[0] == 0x1f && data[1] == 0x8b {
		return true
	}

	// Zstd magic: 0x28, 0xb5, 0x2f, 0xfd
	if len(data) >= 4 && data[0] == 0x28 && data[1] == 0xb5 && data[2] == 0x2f && data[3] == 0xfd {
		return true
	}

	return false
}

// MatchesEncoding reports whether enc is present within the Content-Encoding header value.
func MatchesEncoding(headerValue []byte, enc string) bool {
	if len(headerValue) == 0 || len(enc) == 0 {
		return false
	}

	return strings.Contains(strings.ToLower(string(headerValue)), strings.ToLower(enc))
}

// Compress encodes src into dst using the specified Content-Encoding algorithm ("gzip", "br", "deflate").
func Compress(encoding string, src, dst []byte, level ...int) ([]byte, error) {
	if len(src) == 0 {
		return dst[:0], nil
	}

	switch encoding {
	case "gzip", "x-gzip":
		return Gzip(src, dst, level...)
	case "br":
		return Brotli(src, dst, level...)
	case "deflate":
		return Deflate(src, dst, level...)
	case "identity", "":
		if cap(dst) < len(src) {
			dst = make([]byte, len(src))
		} else {
			dst = dst[:len(src)]
		}

		copy(dst, src)

		return dst, nil
	}

	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "gzip", "x-gzip":
		return Gzip(src, dst, level...)
	case "br":
		return Brotli(src, dst, level...)
	case "deflate":
		return Deflate(src, dst, level...)
	case "identity", "":
		if cap(dst) < len(src) {
			dst = make([]byte, len(src))
		} else {
			dst = dst[:len(src)]
		}

		copy(dst, src)

		return dst, nil

	default:
		return nil, ErrUnsupportedEncoding
	}
}

// CompressScoped encodes src directly into a zero-allocation scoped buffer bound to the lifetime of s.
func CompressScoped(s *borrow.Scope, encoding string, src []byte, level ...int) (borrow.Bytes, error) {
	if len(src) == 0 {
		return borrow.Bytes{}, nil
	}

	switch encoding {
	case "gzip", "x-gzip":
		return GzipScoped(s, src, level...)
	case "br":
		return BrotliScoped(s, src, level...)
	case "deflate":
		return DeflateScoped(s, src, level...)
	case "identity", "":
		if s == nil {
			return borrow.NewBytes(src, nil), nil
		}

		b := s.AllocBytes(len(src))
		copy(b.AsSlice(), src)

		return b, nil
	}

	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "gzip", "x-gzip":
		return GzipScoped(s, src, level...)
	case "br":
		return BrotliScoped(s, src, level...)
	case "deflate":
		return DeflateScoped(s, src, level...)
	case "identity", "":
		if s == nil {
			return borrow.NewBytes(src, nil), nil
		}

		b := s.AllocBytes(len(src))
		copy(b.AsSlice(), src)

		return b, nil

	default:
		return borrow.Bytes{}, ErrUnsupportedEncoding
	}
}

// Gzip compresses src using RFC 1952 (gzip) format into dst.
func Gzip(src, dst []byte, level ...int) ([]byte, error) {
	if len(src) == 0 {
		return dst[:0], nil
	}

	buf := byteBufferStorage.Get()
	defer byteBufferStorage.Put(buf)

	buf.Reset()

	zw := gzipWriterStorage.Get()
	defer gzipWriterStorage.Put(zw)

	zw.Reset(buf)

	if _, err := zw.Write(src); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}

	compressed := buf.Bytes()
	if cap(dst) < len(compressed) {
		dst = make([]byte, len(compressed))
	} else {
		dst = dst[:len(compressed)]
	}

	copy(dst, compressed)

	return dst, nil
}

// GzipScoped compresses src into a zero-allocation scoped buffer bound to s.
func GzipScoped(s *borrow.Scope, src []byte, level ...int) (borrow.Bytes, error) {
	if len(src) == 0 {
		return borrow.Bytes{}, nil
	}

	if s == nil {
		raw, err := Gzip(src, nil, level...)
		if err != nil {
			return borrow.Bytes{}, err
		}

		return borrow.NewBytes(raw, nil), nil
	}

	buf := byteBufferStorage.Get()
	defer byteBufferStorage.Put(buf)

	buf.Reset()

	zw := gzipWriterStorage.Get()
	defer gzipWriterStorage.Put(zw)

	zw.Reset(buf)

	if _, err := zw.Write(src); err != nil {
		return borrow.Bytes{}, err
	}

	if err := zw.Close(); err != nil {
		return borrow.Bytes{}, err
	}

	compressed := buf.Bytes()
	b := s.AllocBytes(len(compressed))
	copy(b.AsSlice(), compressed)

	return b, nil
}

// Deflate compresses src using raw RFC 1951 deflate format into dst.
func Deflate(src, dst []byte, level ...int) ([]byte, error) {
	if len(src) == 0 {
		return dst[:0], nil
	}

	buf := byteBufferStorage.Get()
	defer byteBufferStorage.Put(buf)

	buf.Reset()

	fw := flateWriterStorage.Get()
	defer flateWriterStorage.Put(fw)

	fw.Reset(buf)

	if _, err := fw.Write(src); err != nil {
		return nil, err
	}

	if err := fw.Close(); err != nil {
		return nil, err
	}

	compressed := buf.Bytes()
	if cap(dst) < len(compressed) {
		dst = make([]byte, len(compressed))
	} else {
		dst = dst[:len(compressed)]
	}

	copy(dst, compressed)

	return dst, nil
}

// DeflateScoped compresses src using raw deflate into a zero-allocation scoped buffer bound to s.
func DeflateScoped(s *borrow.Scope, src []byte, level ...int) (borrow.Bytes, error) {
	if len(src) == 0 {
		return borrow.Bytes{}, nil
	}

	if s == nil {
		raw, err := Deflate(src, nil, level...)
		if err != nil {
			return borrow.Bytes{}, err
		}

		return borrow.NewBytes(raw, nil), nil
	}

	buf := byteBufferStorage.Get()
	defer byteBufferStorage.Put(buf)

	buf.Reset()

	fw := flateWriterStorage.Get()
	defer flateWriterStorage.Put(fw)

	fw.Reset(buf)

	if _, err := fw.Write(src); err != nil {
		return borrow.Bytes{}, err
	}

	if err := fw.Close(); err != nil {
		return borrow.Bytes{}, err
	}

	compressed := buf.Bytes()
	b := s.AllocBytes(len(compressed))
	copy(b.AsSlice(), compressed)

	return b, nil
}

// Brotli compresses src using RFC 7932 format into dst.
func Brotli(src, dst []byte, level ...int) ([]byte, error) {
	if len(src) == 0 {
		return dst[:0], nil
	}

	lvl := fasthttp.CompressBrotliDefaultCompression
	if len(level) > 0 {
		lvl = level[0]
	}

	if cap(dst) == 0 {
		return fasthttp.AppendBrotliBytesLevel(nil, src, lvl), nil
	}

	return fasthttp.AppendBrotliBytesLevel(dst[:0], src, lvl), nil
}

// BrotliScoped compresses src using Brotli into a zero-allocation scoped buffer bound to s.
func BrotliScoped(s *borrow.Scope, src []byte, level ...int) (borrow.Bytes, error) {
	if len(src) == 0 {
		return borrow.Bytes{}, nil
	}

	compressed, err := Brotli(src, nil, level...)
	if err != nil {
		return borrow.Bytes{}, err
	}

	if s == nil {
		return borrow.NewBytes(compressed, nil), nil
	}

	b := s.AllocBytes(len(compressed))
	copy(b.AsSlice(), compressed)

	return b, nil
}

// NewWriter returns a pooled [io.WriteCloser] that compresses written data to w
// using the specified Content-Encoding ("gzip", "deflate", "identity").
// When Close is called, the compressor flushes pending bytes and is safely returned to its pool.
func NewWriter(encoding string, w io.Writer, level ...int) (io.WriteCloser, error) {
	if w == nil {
		return nil, errors.New("compress: nil writer")
	}

	enc := strings.ToLower(strings.TrimSpace(encoding))
	switch enc {
	case "gzip", "x-gzip":
		zw := gzipWriterStorage.Get()
		zw.Reset(w)

		return &compressWriteCloser{
			writer: zw,
			release: func() {
				gzipWriterStorage.Put(zw)
			},
		}, nil

	case "deflate":
		fw := flateWriterStorage.Get()
		fw.Reset(w)

		return &compressWriteCloser{
			writer: fw,
			release: func() {
				flateWriterStorage.Put(fw)
			},
		}, nil

	case "identity", "":
		return nopWriteCloser{Writer: w}, nil

	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedEncoding, encoding)
	}
}

type compressWriteCloser struct {
	writer  io.WriteCloser
	release func()
}

func (c *compressWriteCloser) Write(p []byte) (int, error) {
	return c.writer.Write(p)
}

func (c *compressWriteCloser) Close() error {
	var err error
	if c.writer != nil {
		err = c.writer.Close()
	}

	if c.release != nil {
		c.release()
		c.release = nil
	}

	return err
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }
