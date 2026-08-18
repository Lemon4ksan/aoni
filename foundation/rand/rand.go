// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rand implements a 1-CPU-cycle thread-local fast pseudo-random number generator,
// bypassing standard math/rand lock contention, atomic CAS loops, and heap allocations.
package rand

import (
	"time"

	"github.com/valyala/fastrand"
)

// Uint32 returns a fast pseudo-random 32-bit unsigned integer without atomic CAS loops or lock contention.
//
//go:inline
func Uint32() uint32 {
	return fastrand.Uint32()
}

// Uint32n returns a fast pseudo-random 32-bit unsigned integer in range [0, n) using Lemire's multiplication reduction.
//
//go:inline
func Uint32n(n uint32) uint32 {
	return fastrand.Uint32n(n)
}

// Uint64 returns a fast pseudo-random 64-bit unsigned integer with zero heap allocations.
//
//go:inline
func Uint64() uint64 {
	return (uint64(fastrand.Uint32()) << 32) | uint64(fastrand.Uint32())
}

// Intn returns a fast pseudo-random integer in range [0, n) with zero allocations.
//
//go:inline
func Intn(n int) int {
	if n <= 0 {
		return 0
	}

	return int(fastrand.Uint32n(uint32(n)))
}

// FastJitter returns a pseudo-random jitter duration between 0 and maxJitter.
//
//go:inline
func FastJitter(maxJitter time.Duration) time.Duration {
	if maxJitter <= 0 {
		return 0
	}

	return time.Duration(fastrand.Uint32n(uint32(maxJitter)))
}
