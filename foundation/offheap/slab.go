// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package offheap

import (
	"errors"
	"fmt"
	"math/bits"
	"runtime"
	"unsafe"
)

// SlabAllocator manages a fixed pool of off-heap T-typed slots backed by a single OS kernel memory page.
// Unlike [Arena], individual slots can be returned with [SlabAllocator.Free], enabling out-of-order deallocation.
//
// Internally, free/used state is tracked by a packed uint64 bitmap (1 bit per slot).
// [SlabAllocator.Alloc] finds the first free slot with [bits.TrailingZeros64] in O(slots/64) time.
// [SlabAllocator.Free] computes the slot index from the pointer offset in O(1).
//
// SlabAllocator is NOT thread-safe. Use an external mutex for concurrent access.
//
// CRITICAL GC RULE: T MUST be a Plain Old Data (POD) structure.
// See package documentation for the full constraint list.
type SlabAllocator[T any] struct {
	page   unsafe.Pointer // OS kernel-managed memory slab
	bitmap []uint64       // packed bit array: 1=free slot, 0=used slot
	stride int            // bytes between adjacent slots: sizeof(T) rounded up to alignof(T)
	size   int            // sizeof(T) - bytes to zero on alloc/free
	cap    int            // total number of T slots in the slab
	free   int            // number of currently free slots
}

// NewSlabAllocator allocates an off-heap slab that can hold slotCount objects of type T.
// Returns an error if slotCount <= 0, T is a zero-size type, or OS memory allocation fails.
func NewSlabAllocator[T any](slotCount int) (*SlabAllocator[T], error) {
	if slotCount <= 0 {
		return nil, fmt.Errorf("offheap: slotCount must be positive, got %d", slotCount)
	}

	var zero T

	size := int(unsafe.Sizeof(zero))
	if size == 0 {
		return nil, errors.New("offheap: cannot create slab for zero-size type")
	}

	// Stride = sizeof(T) rounded up to alignof(T) so every slot starts at a naturally aligned address.
	align := int(unsafe.Alignof(zero))
	stride := size

	if align > 1 && size%align != 0 {
		stride = size + align - size%align
	}

	page, err := allocKernelPage(stride * slotCount)
	if err != nil {
		return nil, fmt.Errorf("offheap: slab allocation failed: %w", err)
	}

	// Build the free bitmap: 1 bit per slot, initial value 1 (all free).
	words := (slotCount + 63) / 64
	bm := make([]uint64, words)

	// Set all bits to 1 for full words.
	fullWords := slotCount / 64
	for i := range fullWords {
		bm[i] = ^uint64(0)
	}

	// Partial last word: only the bits corresponding to real slots are set.
	if rem := slotCount % 64; rem > 0 {
		bm[fullWords] = (uint64(1) << rem) - 1
	}

	s := &SlabAllocator[T]{
		page:   page,
		bitmap: bm,
		stride: stride,
		size:   size,
		cap:    slotCount,
		free:   slotCount,
	}

	runtime.SetFinalizer(s, (*SlabAllocator[T]).Release)

	return s, nil
}

// Alloc checks out one zero-initialized T slot from the slab.
// Returns nil if all slots are currently in use.
//
// Time complexity: O(slots/64) - scans bitmap words until a free slot is found.
func (s *SlabAllocator[T]) Alloc() *T {
	if s == nil || s.page == nil || s.free == 0 {
		return nil
	}

	for i, word := range s.bitmap {
		if word == 0 {
			continue
		}

		// bits.TrailingZeros64 compiles to TZCNT/BSF on amd64 - single instruction.
		bit := bits.TrailingZeros64(word)
		s.bitmap[i] &^= 1 << uint(bit)
		s.free--

		idx := i*64 + bit
		ptr := unsafe.Add(s.page, idx*s.stride)

		// Zero-initialize the slot before returning it to the caller.
		clear(unsafe.Slice((*byte)(ptr), s.size))

		return (*T)(ptr)
	}

	return nil
}

// Free returns a slot previously obtained via [SlabAllocator.Alloc] back to the slab.
// The slot memory is zeroed before being marked free.
//
// Returns true if the pointer belonged to this slab and was freed, or false if the
// pointer did not originate from this slab (or was nil).
//
// Time complexity: O(1).
func (s *SlabAllocator[T]) Free(p *T) bool {
	if s == nil || s.page == nil || p == nil {
		return false
	}

	// Compute slot index from pointer offset within the slab page.
	offset := int(uintptr(unsafe.Pointer(p)) - uintptr(s.page))
	if offset < 0 || offset >= s.stride*s.cap || offset%s.stride != 0 {
		return false // pointer not from this slab, or misaligned - ignore
	}

	idx := offset / s.stride

	// Zero the slot memory before marking free to prevent stale data reads after reuse.
	clear(unsafe.Slice((*byte)(unsafe.Pointer(p)), s.size))

	word := idx / 64
	bit := uint(idx % 64)
	s.bitmap[word] |= 1 << bit
	s.free++

	return true
}

// Reset marks all slots as free in O(slots/64) time.
// Slot memory is NOT zeroed - it will be zeroed lazily on the next [SlabAllocator.Alloc].
//
// All previously returned pointers are invalidated after Reset.
func (s *SlabAllocator[T]) Reset() {
	if s == nil || s.page == nil {
		return
	}

	fullWords := s.cap / 64

	for i := range fullWords {
		s.bitmap[i] = ^uint64(0)
	}

	if rem := s.cap % 64; rem > 0 {
		s.bitmap[fullWords] = (uint64(1) << rem) - 1
	}

	s.free = s.cap
}

// Len returns the number of currently allocated (in-use) slots.
func (s *SlabAllocator[T]) Len() int {
	if s == nil {
		return 0
	}

	return s.cap - s.free
}

// Cap returns the total slot capacity.
func (s *SlabAllocator[T]) Cap() int {
	if s == nil {
		return 0
	}

	return s.cap
}

// Available returns the number of free slots remaining.
func (s *SlabAllocator[T]) Available() int {
	if s == nil {
		return 0
	}

	return s.free
}

// Release frees the entire slab page back to the OS kernel.
// All previously returned slot pointers are invalidated.
func (s *SlabAllocator[T]) Release() {
	if s != nil && s.page != nil {
		runtime.SetFinalizer(s, nil)
		_ = freeKernelPage(s.page, s.stride*s.cap)
		s.page = nil
		s.bitmap = nil
		s.free = 0
	}
}
