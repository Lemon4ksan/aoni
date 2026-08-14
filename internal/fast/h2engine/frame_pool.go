// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine

import (
	"github.com/lemon4ksan/aoni/internal/offheap"
)

const defaultConnPoolCapacity = 256

// ConnectionFramePool holds per-goroutine off-heap slab allocators for the four
// Plain Old Data HTTP/2 frame types: Ping, Priority, RstStream, and WindowUpdate.
//
// Unlike the global [framePools] (sync.Pool), slabs are not subject to GC eviction,
// which eliminates allocation spikes during garbage collection pauses on high-traffic
// H2 connections. Non-POD frame types (Data, Headers, Continuation, PushPromise,
// GoAway, Settings) fall back to the global sync.Pool automatically.
//
// # Thread Safety
//
// ConnectionFramePool is NOT thread-safe. Each goroutine that allocates frames
// must own an independent pool instance. A typical H2 connection has separate
// read and write loops; each should create and own its own ConnectionFramePool:
//
//	type Conn struct {
//	    readPool  *ConnectionFramePool  // used only by the read goroutine
//	    writePool *ConnectionFramePool  // used only by the write goroutine
//	}
//
// This design avoids any mutex overhead: the slab operations become a pure
// bitmap scan + pointer arithmetic, making them faster than sync.Pool in
// single-goroutine hot paths.
//
// Call [ConnectionFramePool.Release] when the owning goroutine exits or the
// connection closes to return kernel memory pages to the OS.
type ConnectionFramePool struct {
	ping *offheap.SlabAllocator[Ping]
	wu   *offheap.SlabAllocator[WindowUpdate]
	rst  *offheap.SlabAllocator[RstStream]
	prio *offheap.SlabAllocator[Priority]
}

// NewConnectionFramePool creates a ConnectionFramePool with the given slot capacity
// for each POD frame type. If capacity <= 0, defaultConnPoolCapacity (256) is used.
// If any slab allocation fails, the affected type falls back to sync.Pool silently.
func NewConnectionFramePool(capacity int) *ConnectionFramePool {
	if capacity <= 0 {
		capacity = defaultConnPoolCapacity
	}

	p := &ConnectionFramePool{}

	// Each slab is created independently; a failure does not prevent others from initialising.
	if slab, err := offheap.NewSlabAllocator[Ping](capacity); err == nil {
		p.ping = slab
	}

	if slab, err := offheap.NewSlabAllocator[WindowUpdate](capacity); err == nil {
		p.wu = slab
	}

	if slab, err := offheap.NewSlabAllocator[RstStream](capacity); err == nil {
		p.rst = slab
	}

	if slab, err := offheap.NewSlabAllocator[Priority](capacity); err == nil {
		p.prio = slab
	}

	return p
}

// AcquireFrame returns a zero-initialised Frame for the given frameType.
// For the four POD types (Ping, Priority, RstStream, WindowUpdate) the frame is
// allocated from the corresponding off-heap slab; all other types use sync.Pool.
// Falls back to sync.Pool when the slab is exhausted.
//
// Must only be called from the goroutine that owns this pool.
func (p *ConnectionFramePool) AcquireFrame(frameType FrameType) Frame {
	if p == nil {
		return AcquireFrame(frameType)
	}

	switch frameType {
	case FramePing:
		if p.ping != nil {
			if f := p.ping.Alloc(); f != nil {
				f.Reset()
				return f
			}
		}

	case FrameWindowUpdate:
		if p.wu != nil {
			if f := p.wu.Alloc(); f != nil {
				f.Reset()
				return f
			}
		}

	case FrameResetStream:
		if p.rst != nil {
			if f := p.rst.Alloc(); f != nil {
				f.Reset()
				return f
			}
		}

	case FramePriority:
		if p.prio != nil {
			if f := p.prio.Alloc(); f != nil {
				f.Reset()
				return f
			}
		}
	}

	// Non-POD types or exhausted slab - use the global sync.Pool.
	return AcquireFrame(frameType)
}

// ReleaseFrame returns a Frame back to its origin pool.
// POD frames obtained from slabs are returned to the slab; all others go to sync.Pool.
// Passing a frame not owned by this pool (e.g. obtained via AcquireFrame directly)
// is safe and simply releases it to sync.Pool.
//
// Must only be called from the goroutine that owns this pool.
func (p *ConnectionFramePool) ReleaseFrame(fr Frame) {
	if fr == nil {
		return
	}

	if p != nil {
		switch f := fr.(type) {
		case *Ping:
			if p.ping != nil && p.ping.Free(f) {
				return
			}

		case *WindowUpdate:
			if p.wu != nil && p.wu.Free(f) {
				return
			}

		case *RstStream:
			if p.rst != nil && p.rst.Free(f) {
				return
			}

		case *Priority:
			if p.prio != nil && p.prio.Free(f) {
				return
			}
		}
	}

	ReleaseFrame(fr)
}

// Release frees all slab kernel memory pages back to the OS.
// Must be called when the owning goroutine exits or the H2 connection closes.
// All previously obtained frame pointers are invalidated after Release.
func (p *ConnectionFramePool) Release() {
	if p == nil {
		return
	}

	if p.ping != nil {
		p.ping.Release()
		p.ping = nil
	}

	if p.wu != nil {
		p.wu.Release()
		p.wu = nil
	}

	if p.rst != nil {
		p.rst.Release()
		p.rst = nil
	}

	if p.prio != nil {
		p.prio.Release()
		p.prio = nil
	}
}
