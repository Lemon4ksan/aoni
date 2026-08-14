// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package clock provides high-performance coarse atomic time utilities for hot-path TTL and cache expiration checks.
//
// Architectural Concept & Mechanical Sympathy:
// System calls to time.Now() invoke kernel vDSO routines, which incur branch prediction overhead
// and CPU pipeline stalls on extreme-throughput hot paths (1M+ RPS).
// This package runs a dedicated 1ms background ticker that atomically stores the current timestamp
// in an [atomic.Int64], converting time checks into single-cycle atomic loads from CPU L1 cache (0 B/op).
//
// Precision & Safety Invariants:
//   - Resolution is coarse (~1 millisecond), making it ideal for cache TTL checks, connection lifetime tracking,
//     and deadline timeouts.
//   - It MUST NOT be used for microsecond-precision latency tracking or round-trip time (RTT) measurements.
package clock

import (
	"sync/atomic"
	"time"
)

var coarseUnixNano atomic.Int64

func init() {
	coarseUnixNano.Store(time.Now().UnixNano())

	go func() {
		ticker := time.NewTicker(1 * time.Millisecond)
		for t := range ticker.C {
			coarseUnixNano.Store(t.UnixNano())
		}
	}()
}

// CoarseNowNano returns the current Unix time in nanoseconds with ~1ms coarse resolution.
// It executes a sub-nanosecond atomic load from L1 cache instead of invoking vDSO time.Now().
//
//go:inline
//go:nosplit
func CoarseNowNano() int64 {
	return coarseUnixNano.Load()
}

// CoarseTime returns the current coarse timestamp as [time.Time] with ~1ms resolution.
//
//go:inline
//go:nosplit
func CoarseTime() time.Time {
	return time.Unix(0, coarseUnixNano.Load())
}
