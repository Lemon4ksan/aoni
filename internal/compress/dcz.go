// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package compress

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"

	"github.com/lemon4ksan/foundation/borrow"

	"github.com/lemon4ksan/aoni/internal/compress/zstd"
	"github.com/lemon4ksan/aoni/netutil/dict"
)

var (
	// ErrDictionaryMismatch signals that the SHA-256 hash in the stream header does not match the local dictionary.
	ErrDictionaryMismatch = errors.New("compress: dictionary SHA-256 hash mismatch")

	// ErrInvalidDCZHeader signals a corrupted or non-conformant DCZ stream header (RFC 9842 §5).
	ErrInvalidDCZHeader = errors.New("compress: invalid Dictionary-Compressed Zstandard (dcz) header")

	// ErrInvalidDCBHeader signals a corrupted or non-conformant DCB stream header (RFC 9842 §4).
	ErrInvalidDCBHeader = errors.New("compress: invalid Dictionary-Compressed Brotli (dcb) header")
)

// UnzstdDCZ decompresses a Dictionary-Compressed Zstandard payload (RFC 9842 §5)
// using the provided raw dictionary.
func UnzstdDCZ(src, dst, dictBytes []byte) ([]byte, error) {
	if len(src) < 40 {
		return nil, ErrInvalidDCZHeader
	}

	// 1. Verify 8-byte DCZ magic (RFC 9842 §5)
	if !bytes.Equal(src[:8], dict.MagicDCZ[:]) {
		return nil, ErrInvalidDCZHeader
	}

	// 2. Verify 32-byte SHA-256 hash
	expectedHash := sha256.Sum256(dictBytes)
	if subtle.ConstantTimeCompare(src[8:40], expectedHash[:]) != 1 {
		return nil, fmt.Errorf("%w: expected %x, got %x", ErrDictionaryMismatch, expectedHash, src[8:40])
	}

	// 3. Decompress Zstandard payload starting at offset 40
	dec, err := zstd.NewReader(nil,
		zstd.WithDecoderDictRaw(0, dictBytes),
		zstd.WithDecoderDictRaw(1, dictBytes),
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(true),
	)
	if err != nil {
		return nil, err
	}
	defer dec.Close()

	return dec.DecodeAll(src[40:], dst)
}

// UnzstdDCZScoped decompresses a DCZ payload directly into a zero-allocation scoped buffer.
func UnzstdDCZScoped(s *borrow.Scope, src, dictBytes []byte) (borrow.Bytes, error) {
	if len(src) == 0 {
		return borrow.Bytes{}, nil
	}

	if s == nil {
		raw, err := UnzstdDCZ(src, nil, dictBytes)
		if err != nil {
			return borrow.Bytes{}, err
		}

		return borrow.NewBytes(raw, nil), nil
	}

	if len(src) < 40 {
		return borrow.Bytes{}, ErrInvalidDCZHeader
	}

	if !bytes.Equal(src[:8], dict.MagicDCZ[:]) {
		return borrow.Bytes{}, ErrInvalidDCZHeader
	}

	expectedHash := sha256.Sum256(dictBytes)
	if subtle.ConstantTimeCompare(src[8:40], expectedHash[:]) != 1 {
		return borrow.Bytes{}, fmt.Errorf("%w: expected %x, got %x", ErrDictionaryMismatch, expectedHash, src[8:40])
	}

	dec, err := zstd.NewReader(nil,
		zstd.WithDecoderDictRaw(0, dictBytes),
		zstd.WithDecoderDictRaw(1, dictBytes),
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(true),
	)
	if err != nil {
		return borrow.Bytes{}, err
	}
	defer dec.Close()

	initCap := max(len(src)*2, 4096)
	b := s.AllocBytes(initCap)
	dst := b.AsSlice()[:0]

	decompressed, err := dec.DecodeAll(src[40:], dst)
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

// NewDCZReader returns an [io.ReadCloser] that decompresses a streaming DCZ response (RFC 9842 §5).
func NewDCZReader(r io.Reader, dictBytes []byte) (io.ReadCloser, error) {
	if r == nil {
		return nil, errors.New("compress: nil reader")
	}

	// Read and validate 40-byte DCZ fixed header
	var header [40]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDCZHeader, err)
	}

	if !bytes.Equal(header[:8], dict.MagicDCZ[:]) {
		return nil, ErrInvalidDCZHeader
	}

	expectedHash := sha256.Sum256(dictBytes)
	if subtle.ConstantTimeCompare(header[8:40], expectedHash[:]) != 1 {
		return nil, fmt.Errorf("%w: expected %x, got %x", ErrDictionaryMismatch, expectedHash, header[8:40])
	}

	dec, err := zstd.NewReader(r,
		zstd.WithDecoderDictRaw(0, dictBytes),
		zstd.WithDecoderDictRaw(1, dictBytes),
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(true),
	)
	if err != nil {
		return nil, err
	}

	return &decompressReadCloser{
		reader: dec,
		closer: closerOf(r),
		release: func() {
			dec.Close()
		},
	}, nil
}

// WrapDCZHeader prepends the standard 40-byte RFC 9842 §5 header (8-byte magic + 32-byte SHA-256)
// to a raw Zstandard stream.
func WrapDCZHeader(zstdStream []byte, dictHash [32]byte) []byte {
	out := make([]byte, 40+len(zstdStream))
	copy(out[:8], dict.MagicDCZ[:])
	copy(out[8:40], dictHash[:])
	copy(out[40:], zstdStream)

	return out
}
