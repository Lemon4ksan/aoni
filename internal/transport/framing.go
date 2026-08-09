// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transport

import (
	"encoding/binary"
	"errors"
	"io"
)

var (
	// ErrPayloadTooLarge is returned when an incoming binary frame length exceeds MaxPayloadSize.
	ErrPayloadTooLarge = errors.New("transport: frame payload exceeds maximum allowed size")

	// ErrTruncatedHeader is returned when the 5-byte frame header is truncated.
	ErrTruncatedHeader = errors.New("transport: truncated 5-byte frame header")

	// ErrTruncatedPayload is returned when the frame payload stream is truncated.
	ErrTruncatedPayload = errors.New("transport: truncated frame payload")
)

// LengthPrefixedFramer decodes and encodes 5-byte length-prefixed frames:
// [1 byte flags][4 bytes length (BigEndian)][payload bytes].
type LengthPrefixedFramer struct {
	MaxPayloadSize uint32
}

// NewLengthPrefixedFramer initializes a [LengthPrefixedFramer].
func NewLengthPrefixedFramer(maxPayloadSize uint32) *LengthPrefixedFramer {
	if maxPayloadSize == 0 {
		maxPayloadSize = 16 * 1024 * 1024 // 16MB default cap
	}

	return &LengthPrefixedFramer{MaxPayloadSize: maxPayloadSize}
}

// ReadFrame reads a 5-byte header [flags + uint32 length] and payload from r.
func (f *LengthPrefixedFramer) ReadFrame(r io.Reader) (flags byte, payload []byte, err error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return 0, nil, err
		}

		return 0, nil, ErrTruncatedHeader
	}

	flags = header[0]
	length := binary.BigEndian.Uint32(header[1:5])

	if f.MaxPayloadSize > 0 && length > f.MaxPayloadSize {
		return 0, nil, ErrPayloadTooLarge
	}

	if length == 0 {
		return flags, nil, nil
	}

	payload = make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, ErrTruncatedPayload
	}

	return flags, payload, nil
}

// WriteFrame encodes and writes a 5-byte header [flags + uint32 length] and payload to w.
func (f *LengthPrefixedFramer) WriteFrame(w io.Writer, flags byte, payload []byte) (int, error) {
	length := uint32(len(payload))
	if f.MaxPayloadSize > 0 && length > f.MaxPayloadSize {
		return 0, ErrPayloadTooLarge
	}

	var header [5]byte

	header[0] = flags
	binary.BigEndian.PutUint32(header[1:5], length)

	n1, err := w.Write(header[:])
	if err != nil {
		return n1, err
	}

	if length == 0 {
		return n1, nil
	}

	n2, err := w.Write(payload)

	return n1 + n2, err
}
