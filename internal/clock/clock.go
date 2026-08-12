// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package clock provides high-performance coarse atomic time utilities for hot-path TTL and cache expiration checks.
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
// It performs a sub-nanosecond atomic load from L1 cache instead of calling Linux vDSO time.Now().
//
//go:inline
//go:nosplit
func CoarseNowNano() int64 {
	return coarseUnixNano.Load()
}

// CoarseTime returns the current time as time.Time with ~1ms coarse resolution.
//
//go:inline
//go:nosplit
func CoarseTime() time.Time {
	return time.Unix(0, coarseUnixNano.Load())
}
