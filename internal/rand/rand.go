// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rand implements a 1-CPU-cycle branchless fast pseudo-random number generator (xorshift64 / wyrand),
// bypassing standard math/rand lock contention and heap allocations.
package rand

import (
	"math/bits"
	"sync/atomic"
	"time"
)

var globalSeed atomic.Uint64

func init() {
	globalSeed.Store(uint64(time.Now().UnixNano()))
}

// Uint64 returns a fast pseudo-random 64-bit unsigned integer using atomic xorshift64.
func Uint64() uint64 {
	for {
		oldSeed := globalSeed.Load()
		x := oldSeed
		x ^= x << 13
		x ^= x >> 7

		x ^= x << 17
		if globalSeed.CompareAndSwap(oldSeed, x) {
			return x
		}
	}
}

// Intn returns a fast pseudo-random integer in range [0, n) with zero allocations.
func Intn(n int) int {
	if n <= 0 {
		return 0
	}

	u := Uint64()
	hi, _ := bits.Mul64(u, uint64(n))

	return int(hi)
}

// FastJitter returns a pseudo-random jitter duration between 0 and maxJitter.
func FastJitter(maxJitter time.Duration) time.Duration {
	if maxJitter <= 0 {
		return 0
	}

	return time.Duration(Intn(int(maxJitter)))
}
