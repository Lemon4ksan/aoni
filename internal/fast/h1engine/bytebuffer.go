// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1engine

import (
	"io"
	"sync"
)

// ByteBuffer provides a zero-allocation, byte-slice backed expandable buffer.
type ByteBuffer struct {
	// B is the underlying byte slice.
	B []byte
}

// Len returns the number of bytes of the unread portion of the buffer.
func (b *ByteBuffer) Len() int {
	return len(b.B)
}

// Bytes returns the underlying byte slice.
func (b *ByteBuffer) Bytes() []byte {
	return b.B
}

// String returns the contents of the buffer as a string.
func (b *ByteBuffer) String() string {
	return string(b.B)
}

// Reset resets the buffer to be empty.
func (b *ByteBuffer) Reset() {
	b.B = b.B[:0]
}

// Write appends the contents of p to the buffer.
func (b *ByteBuffer) Write(p []byte) (int, error) {
	b.B = append(b.B, p...)
	return len(p), nil
}

// WriteByte appends the byte c to the buffer.
func (b *ByteBuffer) WriteByte(c byte) error {
	b.B = append(b.B, c)
	return nil
}

// WriteString appends the contents of s to the buffer.
func (b *ByteBuffer) WriteString(s string) (int, error) {
	b.B = append(b.B, s...)
	return len(s), nil
}

// Set sets ByteBuffer.B to p.
func (b *ByteBuffer) Set(p []byte) {
	b.B = append(b.B[:0], p...)
}

// SetString sets ByteBuffer.B to s.
func (b *ByteBuffer) SetString(s string) {
	b.B = append(b.B[:0], s...)
}

// ReadFrom reads data from r until EOF and appends it to the buffer.
func (b *ByteBuffer) ReadFrom(r io.Reader) (int64, error) {
	p := b.B
	nStart := int64(len(p))
	nMax := int64(cap(p))
	n := nStart
	if nMax == 0 {
		nMax = 64
		p = make([]byte, nMax)
	} else {
		p = p[:nMax]
	}

	for {
		if n == nMax {
			nMax *= 2
			bNew := make([]byte, nMax)
			copy(bNew, p)
			p = bNew
		}
		nn, err := r.Read(p[n:])
		n += int64(nn)
		if err != nil {
			b.B = p[:n]
			n -= nStart
			if err == io.EOF {
				return n, nil
			}
			return n, err
		}
	}
}

// WriteTo writes data to w until the buffer is drained.
func (b *ByteBuffer) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(b.B)
	return int64(n), err
}

// ByteBufferPool represents a pool of ByteBuffer objects.
type ByteBufferPool struct {
	pool sync.Pool
}

// Get returns a ByteBuffer from the pool.
func (p *ByteBufferPool) Get() *ByteBuffer {
	v := p.pool.Get()
	if v != nil {
		return v.(*ByteBuffer) //nolint:forcetypeassert
	}
	return &ByteBuffer{}
}

// Put returns a ByteBuffer to the pool.
func (p *ByteBufferPool) Put(b *ByteBuffer) {
	if b == nil {
		return
	}
	b.Reset()
	p.pool.Put(b)
}

var defaultByteBufferPool ByteBufferPool

// acquireByteBuffer gets a ByteBuffer from the default pool.
func acquireByteBuffer() *ByteBuffer {
	return defaultByteBufferPool.Get()
}

// releaseByteBuffer puts a ByteBuffer back into the default pool.
func releaseByteBuffer(b *ByteBuffer) {
	defaultByteBufferPool.Put(b)
}
