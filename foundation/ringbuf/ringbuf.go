// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ringbuf implements a lock-free Dmitry Vyukov MPMC (Multi-Producer Multi-Consumer) bounded queue,
// padded to 64-byte CPU cache line boundaries to prevent False Sharing across CPU cores.
package ringbuf

import (
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/cpu"
)

type cell[T any] struct {
	sequence atomic.Uint64
	data     atomic.Pointer[T]
}

// RingBuffer is a lock-free power-of-two capacity MPMC queue.
type RingBuffer[T any] struct {
	_          cpu.CacheLinePad
	enqueuePos atomic.Uint64
	_          cpu.CacheLinePad
	dequeuePos atomic.Uint64
	_          cpu.CacheLinePad
	capacity   uint64
	mask       uint64
	buffer     []cell[T]
}

// NewRingBuffer instantiates a lock-free [RingBuffer] with power-of-two capacity.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	capPow2 := uint64(1)
	for capPow2 < uint64(capacity) {
		capPow2 <<= 1
	}

	buf := make([]cell[T], capPow2)
	for i := uint64(0); i < capPow2; i++ {
		buf[i].sequence.Store(i)
	}

	return &RingBuffer[T]{
		capacity: capPow2,
		mask:     capPow2 - 1,
		buffer:   buf,
	}
}

// Push enqueues item into the ring buffer using atomic CAS.
// Returns false if buffer is full.
func (r *RingBuffer[T]) Push(item *T) bool {
	pos := r.enqueuePos.Load()
	for {
		c := &r.buffer[pos&r.mask]
		seq := c.sequence.Load()
		dif := int64(seq) - int64(pos)

		switch {
		case dif == 0:
			if r.enqueuePos.CompareAndSwap(pos, pos+1) {
				c.data.Store(item)
				c.sequence.Store(pos + 1)

				return true
			}

		case dif < 0:
			return false // Buffer full
		default:
			pos = r.enqueuePos.Load()
		}
	}
}

// Pop dequeues item from the ring buffer using atomic CAS.
// Returns nil if buffer is empty.
func (r *RingBuffer[T]) Pop() *T {
	pos := r.dequeuePos.Load()
	for {
		c := &r.buffer[pos&r.mask]
		seq := c.sequence.Load()
		dif := int64(seq) - int64(pos+1)

		switch {
		case dif == 0:
			if r.dequeuePos.CompareAndSwap(pos, pos+1) {
				item := c.data.Swap(nil)
				c.sequence.Store(pos + r.mask + 1)

				return item
			}

		case dif < 0:
			return nil // Buffer empty
		default:
			pos = r.dequeuePos.Load()
		}
	}
}

// Len returns approximate current element count.
func (r *RingBuffer[T]) Len() int {
	head := r.enqueuePos.Load()
	tail := r.dequeuePos.Load()

	if head < tail {
		return 0
	}

	return int(head - tail)
}

// Ensure unused variable reference to suppress import unused warning if any.
var _ = unsafe.Sizeof(0)
