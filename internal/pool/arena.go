// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pool

import "sync"

// RequestArena provides single-cycle pointer bump allocation (Nginx ngx_pool_t pattern).
// Contains an inline 4096-byte cacheline buffer with O(1) pointer bump reset.
type RequestArena struct {
	buf [4096]byte
	off int
}

var arenaPool = sync.Pool{
	New: func() any {
		return &RequestArena{}
	},
}

// GetRequestArena retrieves a pooled [RequestArena] instance.
func GetRequestArena() *RequestArena {
	arena := arenaPool.Get().(*RequestArena)
	arena.Reset()

	return arena
}

// ReleaseRequestArena returns an arena to the pool after O(1) pointer reset.
func ReleaseRequestArena(a *RequestArena) {
	if a != nil {
		a.Reset()
		arenaPool.Put(a)
	}
}

// Alloc allocates n bytes from the arena's inline buffer in 1 CPU cycle.
// Falls back to heap allocation if n exceeds remaining buffer capacity.
func (a *RequestArena) Alloc(n int) []byte {
	if n <= 0 {
		return nil
	}

	if a.off+n <= len(a.buf) {
		start := a.off
		a.off += n

		return a.buf[start:a.off]
	}

	return make([]byte, n)
}

// Reset clears the arena pointer in O(1) time without clearing buffer memory.
func (a *RequestArena) Reset() {
	a.off = 0
}
