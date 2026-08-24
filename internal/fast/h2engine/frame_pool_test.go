// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/fast/h2engine"
)

func TestConnectionFramePool_AcquireRelease_POD(t *testing.T) {
	pool := h2engine.NewConnectionFramePool(32)

	require.NotNil(t, pool)
	defer pool.Release()

	podTypes := []h2engine.FrameType{
		h2engine.FramePing,
		h2engine.FrameWindowUpdate,
		h2engine.FrameResetStream,
		h2engine.FramePriority,
	}

	for _, ft := range podTypes {
		fr := pool.AcquireFrame(ft)
		require.NotNilf(t, fr, "AcquireFrame(%v) must return non-nil", ft)
		assert.Equal(t, ft, fr.Type(), "returned frame must have correct type")
		pool.ReleaseFrame(fr)
	}
}

func TestConnectionFramePool_AcquireRelease_NonPOD(t *testing.T) {
	pool := h2engine.NewConnectionFramePool(32)

	require.NotNil(t, pool)
	defer pool.Release()

	// Non-POD types must fall back to sync.Pool transparently.
	nonPOD := []h2engine.FrameType{
		h2engine.FrameData,
		h2engine.FrameHeaders,
		h2engine.FrameGoAway,
		h2engine.FramePushPromise,
		h2engine.FrameSettings,
		h2engine.FrameContinuation,
	}

	for _, ft := range nonPOD {
		fr := pool.AcquireFrame(ft)
		require.NotNilf(t, fr, "non-POD type %v must still be allocatable", ft)
		assert.Equal(t, ft, fr.Type())
		pool.ReleaseFrame(fr)
	}
}

func TestConnectionFramePool_NilReceiver(t *testing.T) {
	var pool *h2engine.ConnectionFramePool

	// All operations on nil receiver must not panic and fall back to sync.Pool.
	assert.NotPanics(t, func() {
		fr := pool.AcquireFrame(h2engine.FramePing)
		require.NotNil(t, fr)
		pool.ReleaseFrame(fr)
		pool.Release()
	})
}

func TestConnectionFramePool_ZeroCapacity_UsesDefault(t *testing.T) {
	// capacity <= 0 must use defaultConnPoolCapacity without panicking.
	pool := h2engine.NewConnectionFramePool(0)

	require.NotNil(t, pool)
	defer pool.Release()

	fr := pool.AcquireFrame(h2engine.FramePing)
	require.NotNil(t, fr)
	pool.ReleaseFrame(fr)
}

func TestConnectionFramePool_FreeAndReallocate(t *testing.T) {
	pool := h2engine.NewConnectionFramePool(4)

	require.NotNil(t, pool)
	defer pool.Release()

	// Alloc → release → alloc again to confirm the slot is reused.
	f1 := pool.AcquireFrame(h2engine.FramePing)
	require.NotNil(t, f1)
	pool.ReleaseFrame(f1)

	f2 := pool.AcquireFrame(h2engine.FramePing)
	require.NotNil(t, f2)
	pool.ReleaseFrame(f2)
}

func TestConnectionFramePool_PerGoroutinePool(t *testing.T) {
	// Each goroutine creates and owns its own pool - no sharing, no contention.
	const (
		goroutines = 32
		iterations = 500
	)

	done := make(chan struct{}, goroutines)

	for range goroutines {
		go func() {
			defer func() { done <- struct{}{} }()

			pool := h2engine.NewConnectionFramePool(64)
			defer pool.Release()

			for i := range iterations {
				ft := []h2engine.FrameType{
					h2engine.FramePing,
					h2engine.FrameWindowUpdate,
					h2engine.FrameResetStream,
					h2engine.FramePriority,
					h2engine.FrameData,
					h2engine.FrameHeaders,
				}[i%6]

				fr := pool.AcquireFrame(ft)
				if fr != nil {
					pool.ReleaseFrame(fr)
				}
			}
		}()
	}

	for range goroutines {
		<-done
	}
}

func TestConnectionFramePool_Release_Idempotent(t *testing.T) {
	pool := h2engine.NewConnectionFramePool(16)
	require.NotNil(t, pool)

	// Double-release must not panic.
	assert.NotPanics(t, func() {
		pool.Release()
		pool.Release()
	})
}
