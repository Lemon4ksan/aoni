// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package arena provides a high-performance C-style contiguous memory slab arena allocator
// for temporary request DTOs and byte buffers, achieving O(1) allocation and 0 GC scan overhead.
package arena

import (
	"sync"
	"unsafe"
)

const (
	// DefaultSlabSize defines the standard 64 KB memory slab capacity per arena segment.
	DefaultSlabSize = 64 * 1024
)

var slabPool = sync.Pool{
	New: func() any {
		return &Slab{
			buf: make([]byte, DefaultSlabSize),
		}
	},
}

// Slab represents a contiguous 64 KB byte buffer segment.
type Slab struct {
	buf []byte
	off int
}

// Reset clears the slab offset pointer in O(1) time without heap deallocations.
func (s *Slab) Reset() {
	s.off = 0
}

// AllocBytes allocates n contiguous bytes from the slab.
// Returns nil if remaining capacity is insufficient.
func (s *Slab) AllocBytes(n int) []byte {
	if s.off+n > len(s.buf) {
		return nil
	}

	res := s.buf[s.off : s.off+n]
	s.off += n

	return res
}

// AllocString copies str into contiguous slab memory without heap allocation,
// returning a string referencing the slab memory.
func (s *Slab) AllocString(str string) string {
	if len(str) == 0 {
		return ""
	}

	b := s.AllocBytes(len(str))
	if b == nil {
		return str
	}

	copy(b, str)

	return unsafe.String(unsafe.SliceData(b), len(b))
}

// Arena manages contiguous slab allocations for temporary request lifecycles.
type Arena struct {
	current    *Slab
	isHugePage bool
}

// AcquireArena checks out a slab from pool for current goroutine request execution.
func AcquireArena() *Arena {
	slab := slabPool.Get().(*Slab)
	slab.Reset()

	return &Arena{current: slab}
}

// AcquireHugePageArena allocates a LargePage slab arena backed by 2 MB VirtualAlloc / mmap HugePages.
func AcquireHugePageArena(size int) *Arena {
	if size <= 0 {
		size = 2 * 1024 * 1024 // 2 MB LargePage
	}

	buf := AllocateHugePages(size)

	return &Arena{
		current: &Slab{
			buf: buf,
			off: 0,
		},
		isHugePage: true,
	}
}

// Release returns the arena slab back to memory pool or frees HugePage memory.
func (a *Arena) Release() {
	if a == nil || a.current == nil {
		return
	}

	if a.isHugePage {
		FreeHugePages(a.current.buf)
		a.current = nil

		return
	}

	slabPool.Put(a.current)
	a.current = nil
}

// AllocBytes allocates n bytes within current slab.
func (a *Arena) AllocBytes(n int) []byte {
	if a == nil || a.current == nil {
		return make([]byte, n)
	}

	b := a.current.AllocBytes(n)
	if b == nil {
		return make([]byte, n)
	}

	return b
}

// AllocString allocates a copy of str within current slab.
func (a *Arena) AllocString(str string) string {
	if a == nil || a.current == nil {
		return str
	}

	return a.current.AllocString(str)
}
