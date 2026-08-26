// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/internal/fast/h2engine"
)

// BenchmarkAcquireRelease_SyncPool vs ConnectionFramePool for the 4 POD frame types.

func BenchmarkAcquireRelease_SyncPool_Ping(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		fr := h2engine.AcquireFrame(h2engine.FramePing)
		h2engine.ReleaseFrame(fr)
	}
}

func BenchmarkAcquireRelease_ConnPool_Ping(b *testing.B) {
	pool := h2engine.NewConnectionFramePool(1024)
	defer pool.Release()

	b.ReportAllocs()

	for b.Loop() {
		fr := pool.AcquireFrame(h2engine.FramePing)
		pool.ReleaseFrame(fr)
	}
}

func BenchmarkAcquireRelease_SyncPool_WindowUpdate(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		fr := h2engine.AcquireFrame(h2engine.FrameWindowUpdate)
		h2engine.ReleaseFrame(fr)
	}
}

func BenchmarkAcquireRelease_ConnPool_WindowUpdate(b *testing.B) {
	pool := h2engine.NewConnectionFramePool(1024)
	defer pool.Release()

	b.ReportAllocs()

	for b.Loop() {
		fr := pool.AcquireFrame(h2engine.FrameWindowUpdate)
		pool.ReleaseFrame(fr)
	}
}

func BenchmarkAcquireRelease_SyncPool_RstStream(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		fr := h2engine.AcquireFrame(h2engine.FrameResetStream)
		h2engine.ReleaseFrame(fr)
	}
}

func BenchmarkAcquireRelease_ConnPool_RstStream(b *testing.B) {
	pool := h2engine.NewConnectionFramePool(1024)
	defer pool.Release()

	b.ReportAllocs()

	for b.Loop() {
		fr := pool.AcquireFrame(h2engine.FrameResetStream)
		pool.ReleaseFrame(fr)
	}
}

// BenchmarkAcquireRelease_PerGoroutinePool - the correct usage pattern:
// each goroutine owns its own pool (simulates H2 read/write loop).
func BenchmarkAcquireRelease_PerGoroutinePool_Parallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		pool := h2engine.NewConnectionFramePool(1024)
		defer pool.Release()

		for pb.Next() {
			fr := pool.AcquireFrame(h2engine.FramePing)
			pool.ReleaseFrame(fr)
		}
	})
}
