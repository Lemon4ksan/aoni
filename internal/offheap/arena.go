// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package offheap provides off-heap memory allocation utilities via direct OS kernel memory pages.
package offheap

import (
	"runtime"
	"unsafe"
)

// Arena provides O(1) single-cycle pointer bump allocation over a direct OS kernel memory page.
type Arena struct {
	page   unsafe.Pointer
	offset int32
	size   int32
}

// NewArena allocates an off-heap memory arena of specified byte capacity directly from OS kernel.
func NewArena(size int) (*Arena, error) {
	if size <= 0 {
		size = 2 * 1024 * 1024 // 2MB HugePage default
	}

	page, err := allocKernelPage(size)
	if err != nil {
		return nil, err
	}

	arena := &Arena{
		page:   page,
		offset: 0,
		size:   int32(size),
	}

	runtime.SetFinalizer(arena, (*Arena).Release)

	return arena, nil
}

// Scope executes fn within a Scope-RAII lifetime block, automatically recycling arena page upon return or panic.
func Scope(size int, fn func(arena *Arena)) error {
	arena, err := NewArena(size)
	if err != nil {
		return err
	}

	defer arena.Release()

	fn(arena)

	return nil
}

// Alloc allocates n bytes inside the arena via a single 1-cycle pointer bump.
func (a *Arena) Alloc(n int) unsafe.Pointer {
	if a == nil || a.page == nil || n <= 0 {
		return nil
	}

	reqSize := int32(n)
	if a.offset+reqSize > a.size {
		return nil
	}

	ptr := unsafe.Add(a.page, a.offset)
	a.offset += reqSize

	return ptr
}

// AllocBuffer allocates an [OffHeapBuffer] bound to this arena's memory slab.
func (a *Arena) AllocBuffer(capacity int) *OffHeapBuffer {
	ptr := a.Alloc(capacity)
	if ptr == nil {
		return nil
	}

	return &OffHeapBuffer{
		ptr: ptr,
		len: 0,
		cap: int32(capacity),
	}
}

// Reset clears arena offset in O(1) time without clearing page bytes.
func (a *Arena) Reset() {
	if a != nil {
		a.offset = 0
	}
}

// Release returns the raw physical memory page back to OS kernel.
func (a *Arena) Release() {
	if a != nil && a.page != nil {
		runtime.SetFinalizer(a, nil)
		_ = freeKernelPage(a.page, int(a.size))
		a.page = nil
		a.offset = 0
		a.size = 0
	}
}
