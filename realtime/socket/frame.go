// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/lemon4ksan/foundation/generic"
)

const (
	maxPooledCapacity = 128 * 1024
	defaultMaxFrame   = 10 * 1024 * 1024 // 10 MB limit
)

var (
	// ErrInvalidMagic is returned when a packet header lacks the expected magic bytes.
	ErrInvalidMagic = errors.New("framer: invalid magic bytes")
	// ErrFrameTooLarge is returned when packet payload length exceeds maximum allowed bounds.
	ErrFrameTooLarge = errors.New("framer: frame exceeds maximum size limit")
	// ErrEmptyFrameBuffer is returned when attempting an operation on an empty frame buffer.
	ErrEmptyFrameBuffer = errors.New("framer: empty frame buffer")
)

// FrameBuffer encapsulates pooled memory buffers used during packet framing and processing.
type FrameBuffer struct {
	B []byte
}

// Reset clears the buffer content without deallocating underlying slice storage.
func (fb *FrameBuffer) Reset() {
	if fb != nil {
		fb.B = fb.B[:0]
	}
}

// Len returns the current length of the frame buffer.
func (fb FrameBuffer) Len() int {
	return len(fb.B)
}

// Bytes returns the underlying byte slice.
func (fb FrameBuffer) Bytes() []byte {
	return fb.B
}

// Equal checks equality against raw byte slices or another FrameBuffer.
func (fb FrameBuffer) Equal(other any) bool {
	switch v := other.(type) {
	case *FrameBuffer:
		if v == nil {
			return false
		}
		return bytes.Equal(fb.B, v.B)
	case FrameBuffer:
		return bytes.Equal(fb.B, v.B)
	case []byte:
		return bytes.Equal(fb.B, v)
	}

	return false
}

var frameBufferPool = generic.NewPool(func() *FrameBuffer {
	return &FrameBuffer{
		B: make([]byte, 0, 64*1024),
	}
})

// AcquireFrameBuffer fetches a FrameBuffer from the global pool, resizing to length.
func AcquireFrameBuffer(length int) *FrameBuffer {
	fb := frameBufferPool.Get()
	if fb == nil {
		return &FrameBuffer{B: make([]byte, length)}
	}

	if cap(fb.B) < length {
		fb.B = make([]byte, length)
	} else {
		fb.B = fb.B[:length]
	}

	return fb
}

// ReleaseFrameBuffer recycles a FrameBuffer back to the pool if its capacity is within safe memory limits.
func ReleaseFrameBuffer(fb *FrameBuffer) {
	if fb == nil || cap(fb.B) > maxPooledCapacity {
		return
	}

	fb.B = fb.B[:0]
	frameBufferPool.Put(fb)
}

// Framer defines standard frame encoding and decoding across transport streams.
type Framer interface {
	ReadFrame(r io.Reader) (*FrameBuffer, error)
	WriteFrame(w io.Writer, data []byte) error
}

// Cipher defines symmetric stream or packet encryption and decryption interfaces.
type Cipher interface {
	Encrypt(fb *FrameBuffer) (*FrameBuffer, error)
	Decrypt(fb *FrameBuffer) (*FrameBuffer, error)
}

// LengthPrefixedConfig configures a generic LengthPrefixedFramer.
type LengthPrefixedConfig struct {
	ByteOrder binary.ByteOrder
	Magic     []byte
	MaxLength uint32
}

// LengthPrefixedFramer decodes and encodes frames with a 32-bit length prefix and optional magic header.
type LengthPrefixedFramer struct {
	ByteOrder binary.ByteOrder
	Magic     []byte
	MaxLength uint32
}

// NewLengthPrefixedFramer constructs a LengthPrefixedFramer.
func NewLengthPrefixedFramer(cfg LengthPrefixedConfig) *LengthPrefixedFramer {
	bo := cfg.ByteOrder
	if bo == nil {
		bo = binary.LittleEndian
	}

	maxLen := cfg.MaxLength
	if maxLen == 0 {
		maxLen = defaultMaxFrame
	}

	return &LengthPrefixedFramer{
		ByteOrder: bo,
		Magic:     cfg.Magic,
		MaxLength: maxLen,
	}
}

// ReadFrame reads a length-prefixed frame from r.
func (f *LengthPrefixedFramer) ReadFrame(r io.Reader) (*FrameBuffer, error) {
	headerLen := 4 + len(f.Magic)

	var header [16]byte
	if headerLen > len(header) {
		return nil, errors.New("framer: header size exceeds fixed buffer")
	}

	if _, err := io.ReadFull(r, header[:headerLen]); err != nil {
		return nil, err
	}

	length := f.ByteOrder.Uint32(header[0:4])
	if length > f.MaxLength {
		return nil, fmt.Errorf("%w: length=%d, max=%d", ErrFrameTooLarge, length, f.MaxLength)
	}

	if len(f.Magic) > 0 {
		if !bytes.Equal(header[4:headerLen], f.Magic) {
			return nil, ErrInvalidMagic
		}
	}

	fb := AcquireFrameBuffer(int(length))
	if _, err := io.ReadFull(r, fb.B); err != nil {
		ReleaseFrameBuffer(fb)
		return nil, err
	}

	return fb, nil
}

// WriteFrame writes data with length prefix and magic header to w.
func (f *LengthPrefixedFramer) WriteFrame(w io.Writer, data []byte) error {
	headerLen := 4 + len(f.Magic)

	var header [16]byte
	if headerLen > len(header) {
		return errors.New("framer: header size exceeds fixed buffer")
	}

	f.ByteOrder.PutUint32(header[0:4], uint32(len(data)))

	if len(f.Magic) > 0 {
		copy(header[4:headerLen], f.Magic)
	}

	if _, err := w.Write(header[:headerLen]); err != nil {
		return err
	}

	_, err := w.Write(data)

	return err
}
