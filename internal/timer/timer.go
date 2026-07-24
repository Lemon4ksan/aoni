// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package timer provides a zero-allocation pool for reusable time.Timer instances.
package timer

import (
	"sync"
	"time"
)

var pool sync.Pool

// Acquire fetches a [*time.Timer] from the pool or instantiates a new one, setting its deadline to d.
//
// Postconditions:
//   - Callers MUST release the timer back to the pool via [Release] once expired or read.
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

// Release stops the timer, drains unread channel notifications, and returns the timer instance to the pool.
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
