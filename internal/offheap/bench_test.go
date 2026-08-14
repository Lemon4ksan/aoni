// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package offheap_test

import (
	"bytes"
	"sync"
	"testing"

	"github.com/lemon4ksan/aoni/internal/offheap"
)

var (
	benchSum  uint64
	benchSink []byte
)

var framePool = sync.Pool{
	New: func() any { return new(testFrameHeader) },
}

// ── AllocStruct: Heap vs sync.Pool vs OffHeap ────────────────────────────────

func BenchmarkAllocStruct_Heap(b *testing.B) {
	var sum uint64

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		hdr := new(testFrameHeader)
		hdr.StreamID = uint32(i)
		sum += uint64(hdr.StreamID)
	}

	benchSum = sum
}

func BenchmarkAllocStruct_SyncPool(b *testing.B) {
	var sum uint64

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		hdr := framePool.Get().(*testFrameHeader)
		hdr.StreamID = uint32(i)
		sum += uint64(hdr.StreamID)
		framePool.Put(hdr)
	}

	benchSum = sum
}

func BenchmarkAllocStruct_OffHeap(b *testing.B) {
	arena, err := offheap.NewArena(16 * 1024 * 1024)
	if err != nil {
		b.Fatal(err)
	}

	defer arena.Release()

	var sum uint64

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		hdr := offheap.AllocStruct[testFrameHeader](arena)
		hdr.StreamID = uint32(i)
		sum += uint64(hdr.StreamID)

		arena.Reset()
	}

	benchSum = sum
}

func BenchmarkAllocStruct_OffHeap_Parallel(b *testing.B) {
	pool := offheap.NewArenaPool(4 * 1024 * 1024)

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		var sum uint64

		for pb.Next() {
			a := pool.Acquire()

			hdr := offheap.AllocStruct[testFrameHeader](a)
			if hdr != nil {
				sum += uint64(hdr.StreamID)
			}

			pool.Release(a)
		}

		benchSum += sum
	})
}

// ── OffHeapBuffer vs bytes.Buffer ────────────────────────────────────────────

const benchPayload = "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 42\r\n\r\n"

func BenchmarkBuffer_WriteString_OffHeap(b *testing.B) {
	buf, err := offheap.NewBuffer(64 * 1024)
	if err != nil {
		b.Fatal(err)
	}

	defer buf.Release()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf.Reset()
		_, _ = buf.WriteString(benchPayload)
	}

	benchSink = buf.Bytes()
}

func BenchmarkBuffer_WriteString_BytesBuffer(b *testing.B) {
	var buf bytes.Buffer

	buf.Grow(64 * 1024)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf.Reset()
		_, _ = buf.WriteString(benchPayload)
	}

	benchSink = buf.Bytes()
}

func BenchmarkBuffer_Write_OffHeap(b *testing.B) {
	buf, err := offheap.NewBuffer(64 * 1024)
	if err != nil {
		b.Fatal(err)
	}

	defer buf.Release()

	payload := []byte(benchPayload)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf.Reset()
		_, _ = buf.Write(payload)
	}

	benchSink = buf.Bytes()
}

func BenchmarkBuffer_Write_BytesBuffer(b *testing.B) {
	var buf bytes.Buffer

	buf.Grow(64 * 1024)

	payload := []byte(benchPayload)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf.Reset()
		_, _ = buf.Write(payload)
	}

	benchSink = buf.Bytes()
}

// ── ArenaPool ────────────────────────────────────────────────────────────────

func BenchmarkArenaPool_AcquireRelease(b *testing.B) {
	pool := offheap.NewArenaPool(2 * 1024 * 1024)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		a := pool.Acquire()
		pool.Release(a)
	}
}

func BenchmarkArenaPool_AcquireRelease_Parallel(b *testing.B) {
	pool := offheap.NewArenaPool(2 * 1024 * 1024)

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			a := pool.Acquire()
			pool.Release(a)
		}
	})
}

// ── SlabAllocator ────────────────────────────────────────────────────────────

func BenchmarkSlabAllocator_AllocFree(b *testing.B) {
	slab, err := offheap.NewSlabAllocator[testFrameHeader](1024)
	if err != nil {
		b.Fatal(err)
	}

	defer slab.Release()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		p := slab.Alloc()
		if p != nil {
			p.StreamID = uint32(i)
			slab.Free(p)
		}
	}
}

func BenchmarkSlabAllocator_AllocFree_vs_Heap(b *testing.B) {
	b.Run("Heap", func(b *testing.B) {
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			p := new(testFrameHeader)
			p.StreamID = uint32(i)
			_ = p
		}
	})

	b.Run("SyncPool", func(b *testing.B) {
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			p := framePool.Get().(*testFrameHeader)
			p.StreamID = uint32(i)
			framePool.Put(p)
		}
	})

	b.Run("OffHeapSlab", func(b *testing.B) {
		slab, err := offheap.NewSlabAllocator[testFrameHeader](4096)
		if err != nil {
			b.Fatal(err)
		}

		defer slab.Release()

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			p := slab.Alloc()
			if p != nil {
				p.StreamID = uint32(i)
				slab.Free(p)
			}
		}
	})
}

// ── WriteStruct ───────────────────────────────────────────────────────────────

func BenchmarkWriteStruct_OffHeap(b *testing.B) {
	buf, err := offheap.NewBuffer(64 * 1024)
	if err != nil {
		b.Fatal(err)
	}

	defer buf.Release()

	hdr := &testFrameHeader{StreamID: 1, Length: 16384, Flags: 0x1}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf.Reset()
		_, _ = offheap.WriteStruct(buf, hdr)
	}

	benchSink = buf.Bytes()
}
