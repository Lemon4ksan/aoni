// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package offheap

import (
	"errors"
	"io"
	"runtime"
	"unsafe"
)

var (
	// ErrBufferClosed is returned when operating on a released OffHeapBuffer.
	ErrBufferClosed = errors.New("offheap: buffer is already released")
	// ErrBufferFull is returned when writing past the capacity of an OffHeapBuffer.
	ErrBufferFull = errors.New("offheap: buffer capacity exceeded")
)

// OffHeapBuffer wraps an OS kernel memory page in a compile-time safe struct container.
// Being a struct type, calling append(buf, ...) results in a compile-time type error.
type OffHeapBuffer struct {
	ptr unsafe.Pointer
	len int32
	cap int32
}

// NewBuffer allocates a new OffHeapBuffer of designated capacity directly from OS kernel.
func NewBuffer(capacity int) (*OffHeapBuffer, error) {
	if capacity <= 0 {
		capacity = 64 * 1024 // 64KB default
	}

	ptr, err := allocKernelPage(capacity)
	if err != nil {
		return nil, err
	}

	buf := &OffHeapBuffer{
		ptr: ptr,
		len: 0,
		cap: int32(capacity),
	}

	runtime.SetFinalizer(buf, (*OffHeapBuffer).Release)

	return buf, nil
}

// Write appends bytes p to the OffHeapBuffer (implements io.Writer).
func (b *OffHeapBuffer) Write(p []byte) (int, error) {
	if b == nil || b.ptr == nil {
		return 0, ErrBufferClosed
	}

	n := len(p)
	if n == 0 {
		return 0, nil
	}

	if int(b.len)+n > int(b.cap) {
		return 0, ErrBufferFull
	}

	dst := unsafe.Slice((*byte)(unsafe.Add(b.ptr, b.len)), n)
	copy(dst, p)
	b.len += int32(n)

	return n, nil
}

// WriteString appends string s to the OffHeapBuffer in 1 cycle.
func (b *OffHeapBuffer) WriteString(s string) (int, error) {
	return b.Write([]byte(s))
}

// Read reads up to len(p) bytes from the OffHeapBuffer into p (implements io.Reader).
func (b *OffHeapBuffer) Read(p []byte) (int, error) {
	if b == nil || b.ptr == nil {
		return 0, ErrBufferClosed
	}

	if b.len == 0 {
		return 0, io.EOF
	}

	n := len(p)
	if n > int(b.len) {
		n = int(b.len)
	}

	src := unsafe.Slice((*byte)(b.ptr), n)
	copy(p, src)

	return n, nil
}

// Bytes returns a volatile slice view over the active off-heap buffer data without heap allocation.
func (b *OffHeapBuffer) Bytes() []byte {
	if b == nil || b.ptr == nil || b.len <= 0 {
		return nil
	}

	return unsafe.Slice((*byte)(b.ptr), b.len)
}

// Len returns the current written length in bytes.
func (b *OffHeapBuffer) Len() int {
	if b == nil {
		return 0
	}

	return int(b.len)
}

// Cap returns the total allocated capacity in bytes.
func (b *OffHeapBuffer) Cap() int {
	if b == nil {
		return 0
	}

	return int(b.cap)
}

// Reset clears the buffer length in O(1) time without zeroing memory.
func (b *OffHeapBuffer) Reset() {
	if b != nil {
		b.len = 0
	}
}

// Release returns the raw physical memory page back to the OS kernel.
func (b *OffHeapBuffer) Release() {
	if b != nil && b.ptr != nil {
		runtime.SetFinalizer(b, nil)
		_ = freeKernelPage(b.ptr, int(b.cap))
		b.ptr = nil
		b.len = 0
		b.cap = 0
	}
}
