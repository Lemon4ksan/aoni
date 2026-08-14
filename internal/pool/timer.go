// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pool

import (
	"sync"
	"time"
)

var timerPool sync.Pool

// AcquireTimer fetches a [*time.Timer] from the pool or instantiates a new one, setting its deadline to d.
func AcquireTimer(d time.Duration) *time.Timer {
	v := timerPool.Get()
	if v == nil {
		return time.NewTimer(d)
	}

	t := v.(*time.Timer)
	stopAndDrainTimer(t)
	t.Reset(d)

	return t
}

// ReleaseTimer stops the timer, drains unread channel notifications, and returns the timer instance to the pool.
func ReleaseTimer(t *time.Timer) {
	if t == nil {
		return
	}

	stopAndDrainTimer(t)
	timerPool.Put(t)
}

// stopAndDrainTimer stops the timer and drains any pending channel delivery without blocking.
func stopAndDrainTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}
