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
//
// OffHeapBuffer is NOT thread-safe. Concurrent reads and writes require external synchronization.
//
// Lifetime: memory is valid until [OffHeapBuffer.Release] is called.
// [OffHeapBuffer.Bytes] returns a volatile slice that becomes invalid after Release.
type OffHeapBuffer struct {
	ptr     unsafe.Pointer // Points into OS kernel-managed memory (mmap / VirtualAlloc)
	readPos int32          // Current read cursor position within [0, len)
	len     int32          // Number of bytes written
	cap     int32          // Total allocated capacity in bytes
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
		cap: int32(capacity),
	}

	runtime.SetFinalizer(buf, (*OffHeapBuffer).Release)

	return buf, nil
}

// Write appends bytes p to the OffHeapBuffer (implements [io.Writer]).
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

// WriteString appends string s to the OffHeapBuffer without heap allocations.
func (b *OffHeapBuffer) WriteString(s string) (int, error) {
	if b == nil || b.ptr == nil {
		return 0, ErrBufferClosed
	}

	n := len(s)
	if n == 0 {
		return 0, nil
	}

	if int(b.len)+n > int(b.cap) {
		return 0, ErrBufferFull
	}

	// unsafe.StringData avoids []byte(s) heap allocation - zero-copy string-to-bytes view.
	dst := unsafe.Slice((*byte)(unsafe.Add(b.ptr, b.len)), n)
	copy(dst, unsafe.Slice(unsafe.StringData(s), n))

	b.len += int32(n)

	return n, nil
}

// Read reads up to len(p) bytes from the current read position into p (implements [io.Reader]).
// Returns [io.EOF] when all written bytes have been consumed.
func (b *OffHeapBuffer) Read(p []byte) (int, error) {
	if b == nil || b.ptr == nil {
		return 0, ErrBufferClosed
	}

	available := int(b.len) - int(b.readPos)
	if available <= 0 {
		return 0, io.EOF
	}

	n := len(p)
	if n > available {
		n = available
	}

	src := unsafe.Slice((*byte)(unsafe.Add(b.ptr, b.readPos)), n)
	copy(p, src)

	b.readPos += int32(n)

	return n, nil
}

// Bytes returns a volatile slice view over the active off-heap buffer data without heap allocation.
// The returned slice is invalid after [OffHeapBuffer.Release] is called.
//
//go:nosplit
//go:inline
func (b *OffHeapBuffer) Bytes() []byte {
	if b == nil || b.ptr == nil || b.len <= 0 {
		return nil
	}

	return unsafe.Slice((*byte)(b.ptr), b.len)
}

// RawBytes returns a slice view of the designated length backed by the off-heap allocation.
//
//go:nosplit
//go:inline
func (b *OffHeapBuffer) RawBytes(length int) []byte {
	if b == nil || b.ptr == nil || length <= 0 || int32(length) > b.cap {
		return nil
	}

	b.len = int32(length)

	return unsafe.Slice((*byte)(b.ptr), length)
}

// Len returns the number of bytes written to the buffer.
//
//go:nosplit
//go:inline
func (b *OffHeapBuffer) Len() int {
	if b == nil {
		return 0
	}

	return int(b.len)
}

// Cap returns the total allocated capacity in bytes.
//
//go:nosplit
//go:inline
func (b *OffHeapBuffer) Cap() int {
	if b == nil {
		return 0
	}

	return int(b.cap)
}

// Reset resets both the write length and the read cursor to zero in O(1) time, without zeroing memory.
// The buffer can be reused immediately after Reset.
//
//go:nosplit
//go:inline
func (b *OffHeapBuffer) Reset() {
	if b != nil {
		b.len = 0
		b.readPos = 0
	}
}

// RewindRead resets only the read cursor to the beginning without clearing written data.
// This allows the same content to be re-read without overwriting it.
//
//go:nosplit
//go:inline
func (b *OffHeapBuffer) RewindRead() {
	if b != nil {
		b.readPos = 0
	}
}

// Release returns the raw physical memory page back to the OS kernel.
// After Release, all slice views obtained via [OffHeapBuffer.Bytes] are invalid.
func (b *OffHeapBuffer) Release() {
	if b != nil && b.ptr != nil {
		runtime.SetFinalizer(b, nil)
		_ = freeKernelPage(b.ptr, int(b.cap))
		b.ptr = nil
		b.len = 0
		b.readPos = 0
		b.cap = 0
	}
}
