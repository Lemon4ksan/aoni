// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package timer_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/timer"
)

func TestTimer_AcquireAndRelease(t *testing.T) {
	t.Parallel()

	// 1. Acquire timer with short duration
	t1 := timer.Acquire(10 * time.Millisecond)
	require.NotNil(t, t1)

	// Wait for expiration
	select {
	case <-t1.C:
		// Expired as expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timer did not fire in expected duration")
	}

	// Release back to pool
	timer.Release(t1)
}

func TestTimer_ReuseFromPool(t *testing.T) {
	t.Parallel()

	// Acquire and release immediately
	t1 := timer.Acquire(500 * time.Millisecond)
	timer.Release(t1)

	// Acquire again (should reuse t1 from sync.Pool)
	t2 := timer.Acquire(10 * time.Millisecond)
	require.NotNil(t, t2)

	start := time.Now()
	select {
	case <-t2.C:
		assert.GreaterOrEqual(t, time.Since(start), 10*time.Millisecond)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("reused timer did not fire correctly")
	}

	timer.Release(t2)
}

func TestTimer_EarlyRelease(t *testing.T) {
	t.Parallel()

	// Acquire long duration timer
	t1 := timer.Acquire(5 * time.Second)

	// Sleep slightly and release before firing
	time.Sleep(5 * time.Millisecond)
	timer.Release(t1)

	// Acquire new timer with short duration to ensure channel is clear
	t2 := timer.Acquire(5 * time.Millisecond)

	select {
	case <-t2.C:
		// Fired correctly
	case <-time.After(50 * time.Millisecond):
		t.Fatal("timer failed to fire after early release")
	}

	timer.Release(t2)
}

func TestTimer_ReleaseNil(t *testing.T) {
	t.Parallel()

	// Releasing nil timer must be a safe no-op
	assert.NotPanics(t, func() {
		timer.Release(nil)
	})
}
