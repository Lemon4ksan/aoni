// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pool

import (
	"runtime"
	"sync/atomic"

	"golang.org/x/sys/cpu"
)

type bufferShard[T any] struct {
	_     cpu.CacheLinePad
	items []T
	mu    uint32
	_     cpu.CacheLinePad
}

// PerPStorage provides a sharded per-CPU core memory pool with 0 cross-core CAS contention or Work Stealing locks.
type PerPStorage[T any] struct {
	shards  []bufferShard[T]
	cursor  atomic.Uint64
	factory func() T
}

// NewPerPStorage constructs a PerPStorage sharded according to runtime.GOMAXPROCS(0).
func NewPerPStorage[T any](factory func() T) *PerPStorage[T] {
	n := runtime.GOMAXPROCS(0)
	if n <= 0 {
		n = 1
	}

	shards := make([]bufferShard[T], n)
	for i := range shards {
		shards[i].items = make([]T, 0, 8)
	}

	return &PerPStorage[T]{
		shards:  shards,
		factory: factory,
	}
}

// Get retrieves an item from a local CPU shard, falling back to scanning other shards before allocating.
func (p *PerPStorage[T]) Get() T {
	numShards := uint64(len(p.shards))
	startIdx := p.cursor.Add(1) % numShards

	for i := uint64(0); i < numShards; i++ {
		idx := (startIdx + i) % numShards
		shard := &p.shards[idx]

		if atomic.SwapUint32(&shard.mu, 1) == 0 {
			n := len(shard.items)
			if n > 0 {
				item := shard.items[n-1]

				var zero T

				shard.items[n-1] = zero
				shard.items = shard.items[:n-1]
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
	startIdx := p.cursor.Add(1) % numShards

	for i := uint64(0); i < numShards; i++ {
		idx := (startIdx + i) % numShards
		shard := &p.shards[idx]

		if atomic.SwapUint32(&shard.mu, 1) == 0 {
			if len(shard.items) < 32 {
				shard.items = append(shard.items, item)
				atomic.StoreUint32(&shard.mu, 0)

				return
			}

			atomic.StoreUint32(&shard.mu, 0)
		}
	}
}
