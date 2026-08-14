// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ringbuf

import (
	"sync/atomic"

	"golang.org/x/sys/cpu"
)

// SPSCRingBuffer implements a Single-Producer Single-Consumer lock-free ring buffer.
//
// Memory Architecture & Hardware Alignment:
//   - Zero CAS (CompareAndSwap) Operations: Eliminates atomic CAS loop contention.
//   - Cache Line Padding: Producer cursor (head) and Consumer cursor (tail) are separated
//     by 64-byte cpu.CacheLinePad boundaries to prevent False Sharing across CPU cores.
//   - Bitwise Masking: Buffer capacity is rounded up to the nearest power of two,
//     enabling 1-cycle bitwise AND (& mask) indexing instead of costly division (% cap).
type SPSCRingBuffer[T any] struct {
	_    cpu.CacheLinePad
	head atomic.Uint64 // Written ONLY by Producer thread
	_    cpu.CacheLinePad
	tail atomic.Uint64 // Written ONLY by Consumer thread
	_    cpu.CacheLinePad

	mask   uint64
	buffer []atomic.Pointer[T]
}

// NewSPSCRingBuffer constructs an [SPSCRingBuffer] with the requested capacity.
// Capacity is automatically rounded up to the next power of two.
func NewSPSCRingBuffer[T any](capacity int) *SPSCRingBuffer[T] {
	if capacity < 2 {
		capacity = 2
	}

	capPow2 := uint64(1)
	for capPow2 < uint64(capacity) {
		capPow2 <<= 1
	}

	return &SPSCRingBuffer[T]{
		mask:   capPow2 - 1,
		buffer: make([]atomic.Pointer[T], capPow2),
	}
}

// Push enqueues item into the ring buffer.
//
// Thread Safety Invariant: Must be invoked from EXACTLY ONE Producer goroutine.
// Returns false if the buffer is full.
func (s *SPSCRingBuffer[T]) Push(item *T) bool {
	head := s.head.Load()
	tail := s.tail.Load()

	if head-tail > s.mask {
		return false // Buffer is full
	}

	idx := head & s.mask
	s.buffer[idx].Store(item)
	s.head.Store(head + 1)

	return true
}

// Pop dequeues item from the ring buffer.
//
// Thread Safety Invariant: Must be invoked from EXACTLY ONE Consumer goroutine.
// Returns nil if the buffer is empty.
func (s *SPSCRingBuffer[T]) Pop() *T {
	tail := s.tail.Load()
	head := s.head.Load()

	if tail == head {
		return nil // Buffer is empty
	}

	idx := tail & s.mask
	item := s.buffer[idx].Swap(nil)
	s.tail.Store(tail + 1)

	return item
}

// Len returns the current number of items buffered in the queue.
func (s *SPSCRingBuffer[T]) Len() int {
	head := s.head.Load()
	tail := s.tail.Load()

	if head >= tail {
		return int(head - tail)
	}

	return 0
}

// Cap returns the total power-of-two storage capacity of the ring buffer.
func (s *SPSCRingBuffer[T]) Cap() int {
	return int(s.mask + 1)
}

// IsFull reports whether the ring buffer cannot accept more items without popping.
func (s *SPSCRingBuffer[T]) IsFull() bool {
	return s.Len() >= s.Cap()
}

// IsEmpty reports whether the ring buffer contains zero items.
func (s *SPSCRingBuffer[T]) IsEmpty() bool {
	return s.Len() == 0
}
