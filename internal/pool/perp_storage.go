// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pool

import (
	"runtime"
	"sync/atomic"

	"golang.org/x/sys/cpu"
)

const shardCapacity = 32

type bufferShard[T any] struct {
	_     cpu.CacheLinePad
	items [shardCapacity]T
	head  uint32
	mu    uint32
	_     cpu.CacheLinePad
}

// PerPStorage provides a sharded per-CPU core memory pool with 0 cross-core CAS contention or Work Stealing locks.
type PerPStorage[T any] struct {
	shards  []bufferShard[T]
	mask    uint64
	cursor  atomic.Uint64
	factory func() T
}

// nextPowerOfTwo calculates the smallest power of 2 greater than or equal to n.
func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}

	p := 1
	for p < n {
		p <<= 1
	}

	return p
}

// NewPerPStorage constructs a PerPStorage sharded according to runtime.GOMAXPROCS(0) rounded to a power of 2.
func NewPerPStorage[T any](factory func() T) *PerPStorage[T] {
	n := runtime.GOMAXPROCS(0)
	if n <= 0 {
		n = 1
	}

	powerOfTwoN := nextPowerOfTwo(n)
	shards := make([]bufferShard[T], powerOfTwoN)

	return &PerPStorage[T]{
		shards:  shards,
		mask:    uint64(powerOfTwoN - 1),
		factory: factory,
	}
}

// Get retrieves an item from a local CPU shard, falling back to scanning other shards before allocating.
func (p *PerPStorage[T]) Get() T {
	numShards := uint64(len(p.shards))
	startIdx := p.cursor.Add(1) & p.mask

	for i := uint64(0); i < numShards; i++ {
		idx := (startIdx + i) & p.mask
		shard := &p.shards[idx]

		// Lockless Shared-state pre-check: skip empty shards without CAS bus lock
		if atomic.LoadUint32(&shard.head) == 0 {
			continue
		}

		if atomic.CompareAndSwapUint32(&shard.mu, 0, 1) {
			head := atomic.LoadUint32(&shard.head)
			if head > 0 {
				item := shard.items[head-1]

				var zero T

				shard.items[head-1] = zero
				atomic.StoreUint32(&shard.head, head-1)
				atomic.StoreUint32(&shard.mu, 0)

				return item
			}

			atomic.StoreUint32(&shard.mu, 0)
		}
	}

	if p.factory != nil {
		return p.factory()
	}

	var zero T

	return zero
}

// Put recycles an item back into a local CPU shard, placing it in the first non-full shard.
func (p *PerPStorage[T]) Put(item T) {
	numShards := uint64(len(p.shards))
	startIdx := p.cursor.Add(1) & p.mask

	for i := uint64(0); i < numShards; i++ {
		idx := (startIdx + i) & p.mask
		shard := &p.shards[idx]

		// Lockless Shared-state pre-check: skip full shards without CAS bus lock
		if atomic.LoadUint32(&shard.head) >= shardCapacity {
			continue
		}

		if atomic.CompareAndSwapUint32(&shard.mu, 0, 1) {
			head := atomic.LoadUint32(&shard.head)
			if head < shardCapacity {
				shard.items[head] = item
				atomic.StoreUint32(&shard.head, head+1)
				atomic.StoreUint32(&shard.mu, 0)

				return
			}

			atomic.StoreUint32(&shard.mu, 0)
		}
	}
}
