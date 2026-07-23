// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

// Package timer provides a zero-allocation pool for reusable time.Timer instances.
package timer

import (
	"sync"
	"time"
)

var pool sync.Pool

// Acquire fetches a [time.Timer] from the internal pool or instantiates a new one,
// resetting its expiration deadline to d.
//
// The caller MUST release the timer back to the pool using [Release] once it expires
// or when its channel reading is complete. Failure to release will cause memory leaks.
func Acquire(d time.Duration) *time.Timer {
	v := pool.Get()
	if v == nil {
		return time.NewTimer(d)
	}

	t := v.(*time.Timer)
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}

	t.Reset(d)

	return t
}

// Release stops the timer, drains any unread notification from its channel,
// and returns the timer instance to the pool for reuse.
//
// Preconditions: t must not be nil. Do not read from t.C after releasing.
func Release(t *time.Timer) {
	if t == nil {
		return
	}

	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}

	pool.Put(t)
}
