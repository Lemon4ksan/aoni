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

	"github.com/lemon4ksan/aoni/internal/compress/brotli"
	"github.com/lemon4ksan/aoni/netutil/dict"
)

// UnbrotliDCB decompresses a Dictionary-Compressed Brotli payload (RFC 9842 §4)
// using the provided raw dictionary.
func UnbrotliDCB(src, dst, dictBytes []byte) ([]byte, error) {
	if len(src) < 36 {
		return nil, ErrInvalidDCBHeader
	}

	// 1. Verify 4-byte DCB magic: 0xff, 0x44, 0x43, 0x42 (RFC 9842 §4)
	if !bytes.Equal(src[:4], dict.MagicDCB[:]) {
		return nil, ErrInvalidDCBHeader
	}

	// 2. Verify 32-byte SHA-256 hash
	expectedHash := sha256.Sum256(dictBytes)
	if subtle.ConstantTimeCompare(src[4:36], expectedHash[:]) != 1 {
		return nil, fmt.Errorf("%w: expected %x, got %x", ErrDictionaryMismatch, expectedHash, src[4:36])
	}

	// 3. Decompress Brotli payload starting at offset 36
	return brotli.Decompress(dst, src[36:])
}

// UnbrotliDCBScoped decompresses a DCB payload directly into a zero-allocation scoped buffer.
func UnbrotliDCBScoped(s *borrow.Scope, src, dictBytes []byte) (borrow.Bytes, error) {
	if len(src) == 0 {
		return borrow.Bytes{}, nil
	}

	if s == nil {
		raw, err := UnbrotliDCB(src, nil, dictBytes)
		if err != nil {
			return borrow.Bytes{}, err
		}

		return borrow.NewBytes(raw, nil), nil
	}

	if len(src) < 36 {
		return borrow.Bytes{}, ErrInvalidDCBHeader
	}

	if !bytes.Equal(src[:4], dict.MagicDCB[:]) {
		return borrow.Bytes{}, ErrInvalidDCBHeader
	}

	expectedHash := sha256.Sum256(dictBytes)
	if subtle.ConstantTimeCompare(src[4:36], expectedHash[:]) != 1 {
		return borrow.Bytes{}, fmt.Errorf("%w: expected %x, got %x", ErrDictionaryMismatch, expectedHash, src[4:36])
	}

	return brotli.DecompressScoped(s, src[36:])
}

// NewDCBReader returns an [io.ReadCloser] that decompresses a streaming DCB response (RFC 9842 §4).
func NewDCBReader(r io.Reader, dictBytes []byte) (io.ReadCloser, error) {
	if r == nil {
		return nil, errors.New("compress: nil reader")
	}

	// Read and validate 36-byte DCB fixed header
	var header [36]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDCBHeader, err)
	}

	if !bytes.Equal(header[:4], dict.MagicDCB[:]) {
		return nil, ErrInvalidDCBHeader
	}

	expectedHash := sha256.Sum256(dictBytes)
	if subtle.ConstantTimeCompare(header[4:36], expectedHash[:]) != 1 {
		return nil, fmt.Errorf("%w: expected %x, got %x", ErrDictionaryMismatch, expectedHash, header[4:36])
	}

	br := brotli.AcquireReader(r)

	return &decompressReadCloser{
		reader: br,
		closer: closerOf(r),
		release: func() {
			brotli.ReleaseReader(br)
		},
	}, nil
}

// WrapDCBHeader prepends the standard 36-byte RFC 9842 §4 header (4-byte magic + 32-byte SHA-256)
// to a raw Brotli stream.
func WrapDCBHeader(brotliStream []byte, dictHash [32]byte) []byte {
	out := make([]byte, 36+len(brotliStream))
	copy(out[:4], dict.MagicDCB[:])
	copy(out[4:36], dictHash[:])
	copy(out[36:], brotliStream)

	return out
}
