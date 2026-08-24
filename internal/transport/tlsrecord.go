// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/lemon4ksan/foundation/borrow"
)

const (
	// TLS 1.3 Record Layer constants (RFC 8446 §5.1 & §5.2)
	RecordTypeApplicationData uint8  = 0x17
	RecordTypeHandshake       uint8  = 0x16
	RecordTypeAlert           uint8  = 0x15
	RecordTypeHeartbeat       uint8  = 0x18
	LegacyVersionTLS12        uint16 = 0x0303

	// MaxTLSRecordPayload is the maximum ciphertext length for a TLS 1.3 record (2^14 + 256 octets).
	MaxTLSRecordPayload = 16384 + 256

	// RecordHeaderSize is the fixed 5-byte TLS record framing header.
	RecordHeaderSize = 5
)

var (
	ErrInvalidRecordLength = errors.New("tlsrecord: record length exceeds protocol maximum")
	ErrTruncatedRecord     = errors.New("tlsrecord: truncated record stream")
	ErrEmptyPlaintext      = errors.New("tlsrecord: decrypted inner plaintext is empty")
	ErrInvalidAuthTag      = errors.New("tlsrecord: AEAD authentication tag verification failed")
)

// RecordFramer manages zero-copy, in-place TLS 1.3 Record Layer (RFC 8446 §5.2)
// encryption and decryption directly within a [borrow.Scope] memory arena.
type RecordFramer struct {
	headerBuf [RecordHeaderSize]byte
	nonceBuf  [12]byte
}

// NewRecordFramer instantiates an in-place TLS 1.3 Record Framer.
func NewRecordFramer() *RecordFramer {
	return &RecordFramer{}
}

// ComputeNonceXOR XORs the 8-byte sequence number into the rightmost 8 bytes of the 12-byte IV (RFC 8446 §5.3).
func ComputeNonceXOR(iv []byte, seq uint64, dst *[12]byte) {
	copy(dst[:], iv)
	binary.BigEndian.PutUint64(dst[4:12], binary.BigEndian.Uint64(dst[4:12])^seq)
}

// ReadRecordScoped reads an encrypted TLS 1.3 record from reader, decrypts the ciphertext in-place
// inside the provided [borrow.Scope] arena, and returns the zero-copy plaintext and inner content type.
//
// In-Place Memory Flow (Zero-Copy):
//  1. Read 5-byte header [Type: 0x17][Version: 0x0303][Length: 2 bytes].
//  2. Allocate exactly Length bytes in scope arena (`scope.AllocBytes(Length)`).
//  3. Stream ciphertext from socket directly into arena memory (`io.ReadFull`).
//  4. Execute in-place AEAD Open: `aead.Open(buf[:0], nonce, buf, header)`.
//  5. Strip padding and extract inner content type from the tail in 0 allocations.
func (f *RecordFramer) ReadRecordScoped(
	r io.Reader,
	aead cipher.AEAD,
	iv []byte,
	seq uint64,
	scope *borrow.Scope,
) (plaintext []byte, innerType uint8, err error) {
	if _, err := io.ReadFull(r, f.headerBuf[:]); err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrTruncatedRecord, err)
	}

	length := binary.BigEndian.Uint16(f.headerBuf[3:5])
	if length > MaxTLSRecordPayload {
		return nil, 0, ErrInvalidRecordLength
	}

	if length < uint16(aead.Overhead()+1) {
		return nil, 0, ErrTruncatedRecord
	}

	borrowed := scope.AllocBytes(int(length))
	rawBuf := borrowed.Bytes()

	if _, err := io.ReadFull(r, rawBuf); err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrTruncatedRecord, err)
	}

	ComputeNonceXOR(iv, seq, &f.nonceBuf)

	// In-Place Decryption: dst overlaps rawBuf[:0], completely eliminating memory copies
	decrypted, err := aead.Open(rawBuf[:0], f.nonceBuf[:], rawBuf, f.headerBuf[:])
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrInvalidAuthTag, err)
	}

	// Scan backwards to strip trailing zero padding and extract inner content type (RFC 8446 §5.4)
	padIdx := len(decrypted) - 1
	for padIdx >= 0 && decrypted[padIdx] == 0 {
		padIdx--
	}

	if padIdx < 0 {
		return nil, 0, ErrEmptyPlaintext
	}

	innerType = decrypted[padIdx]
	plaintext = decrypted[:padIdx]

	return plaintext, innerType, nil
}

// WriteRecordScoped encrypts plaintext with innerType in-place within scope and writes the TLS 1.3 record to w.
func (f *RecordFramer) WriteRecordScoped(
	w io.Writer,
	aead cipher.AEAD,
	iv []byte,
	seq uint64,
	innerType uint8,
	plaintext []byte,
	scope *borrow.Scope,
) error {
	payloadLen := len(plaintext) + 1 + aead.Overhead()
	totalLen := RecordHeaderSize + payloadLen
	if totalLen > MaxTLSRecordPayload+RecordHeaderSize {
		return ErrInvalidRecordLength
	}

	borrowed := scope.AllocBytes(totalLen)
	buf := borrowed.Bytes()

	buf[0] = RecordTypeApplicationData
	binary.BigEndian.PutUint16(buf[1:3], LegacyVersionTLS12)
	binary.BigEndian.PutUint16(buf[3:5], uint16(payloadLen))

	innerPlain := buf[RecordHeaderSize : RecordHeaderSize+len(plaintext)+1]
	copy(innerPlain, plaintext)
	innerPlain[len(plaintext)] = innerType

	ComputeNonceXOR(iv, seq, &f.nonceBuf)

	_ = aead.Seal(innerPlain[:0], f.nonceBuf[:], innerPlain, buf[:RecordHeaderSize])

	_, err := w.Write(buf)
	return err
}
