// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package offheap

import "unsafe"

// StructBytes returns a volatile byte slice view over the raw memory of s without allocation.
// The returned slice shares memory with s and becomes invalid when s is freed, reset, or goes out of scope.
//
// This is useful for writing a POD struct directly to an [OffHeapBuffer] or any [io.Writer]
// without an intermediate encoding step.
//
// T MUST be a POD type (no heap pointers, strings, slices, maps, or channels).
// See package documentation for the full constraint list.
//
//go:nosplit
func StructBytes[T any](s *T) []byte {
	if s == nil {
		return nil
	}

	return unsafe.Slice((*byte)(unsafe.Pointer(s)), unsafe.Sizeof(*s))
}

// CastBytes reinterprets a byte slice as a typed slice of T without allocation.
// The returned slice shares underlying memory with b - no data is copied.
//
// len(b) should be an exact multiple of sizeof(T); any trailing bytes that do not
// fill a complete T are silently ignored.
//
// T MUST be a POD type. The result is invalidated if the backing memory of b is freed.
//
//go:nosplit
func CastBytes[T any](b []byte) []T {
	if len(b) == 0 {
		return nil
	}

	var zero T

	size := int(unsafe.Sizeof(zero))
	if size == 0 {
		return nil
	}

	count := len(b) / size
	if count == 0 {
		return nil
	}

	return unsafe.Slice((*T)(unsafe.Pointer(&b[0])), count)
}

// WriteStruct appends the raw memory bytes of POD struct s to b without heap allocation.
// It is equivalent to a zero-copy binary serialization of s into the buffer.
//
// T MUST be a POD type.
func WriteStruct[T any](b *OffHeapBuffer, s *T) (int, error) {
	if s == nil {
		return 0, nil
	}

	return b.Write(StructBytes(s))
}
