// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package arena

import (
	"io"
	"sync"
	"unsafe"
)

const ArenaSize = 32 * 1024 // 32 KB

// Arena provides an ultra-fast, zero-allocation memory pool designed to bypass Go's Garbage Collector
// in highly concurrent hot paths.
//
// By borrowing pre-allocated [ArenaSize] byte blocks from a [sync.Pool], it allows sub-allocations
// (such as structs, temporary buffers, or decoded data) to be linearly packed into a continuous memory segment.
// This completely eliminates heap allocations and GC pressure for short-lived transactional data.
//
// Memory Safety & Lifetime Rules:
// 1. Memory is ephemeral. All objects allocated via an Arena become invalid the moment [Release] is called.
// 2. You MUST NOT retain pointers to any Arena-allocated objects beyond the scope of the [borrow.Scope] or transaction.
// 3. Returning or leaking Arena pointers to async goroutines will result in fatal memory corruption and data races.
type Arena struct {
	buf    []byte
	offset int
}

var arenaPool = sync.Pool{
	New: func() any {
		return &Arena{
			buf: make([]byte, ArenaSize),
		}
	},
}

// Acquire borrows an Arena from the global memory pool.
func Acquire() *Arena {
	return arenaPool.Get().(*Arena)
}

// Release resets the Arena's offset and returns it to the global memory pool.
//
// WARNING: Calling this function immediately invalidates ALL pointers previously created via [Alloc].
func Release(a *Arena) {
	a.offset = 0
	arenaPool.Put(a)
}

func (a *Arena) Reset() {
	a.offset = 0
}

func (a *Arena) AllocBytes(n int) []byte {
	if a.offset+n > len(a.buf) {
		return make([]byte, n)
	}

	start := a.offset
	a.offset += n

	return a.buf[start:a.offset]
}

// Alloc performs a zero-allocation, linearly packed instantiation of type T.
//
// Architecture & Mechanics:
// Unlike Go's standard `new(T)` which escapes to the heap and triggers GC tracking,
// Alloc uses `unsafe.Pointer` math to carve out a slice of the pre-allocated [Arena.buf] slice.
// It enforces 8-byte word alignment (`a.offset = (a.offset + 7) &^ 7`) to prevent CPU unaligned access panics
// on ARM64 architectures.
//
// Fallback:
// If the requested size exceeds the remaining capacity of the current [Arena] block (e.g. >32KB),
// the allocator gracefully degrades to a standard heap allocation `new(T)`.
//
// DANGER: DO NOT RETAIN POINTERS
// The returned pointer references pooled memory. It must never outlive the call to [Release].
func Alloc[T any](a *Arena) *T {
	var zero T

	size := unsafe.Sizeof(zero)
	a.offset = (a.offset + 7) &^ 7

	if a.offset+int(size) > len(a.buf) {
		return new(T)
	}

	ptr := unsafe.Pointer(&a.buf[a.offset])
	a.offset += int(size)

	return (*T)(ptr)
}

func (a *Arena) ReadAll(r io.Reader) ([]byte, error) {
	start := a.offset

	for {
		if a.offset == len(a.buf) {
			b, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}

			return append(a.buf[start:a.offset], b...), nil
		}

		n, err := r.Read(a.buf[a.offset:])
		a.offset += n

		if err != nil {
			if err == io.EOF {
				return a.buf[start:a.offset], nil
			}

			return nil, err
		}
	}
}
