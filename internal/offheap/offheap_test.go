// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package offheap_test

import (
	"io"
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/offheap"
)

// ── Shared test types ────────────────────────────────────────────────────────

type testFrameHeader struct {
	StreamID uint32
	Length   uint32
	Flags    uint8
	Type     uint8
}

type testFastHeaderSlot struct {
	Key    [32]byte
	Val    [64]byte
	KeyLen uint8
	ValLen uint8
}

// ── OffHeapBuffer ─────────────────────────────────────────────────────────────

func TestOffHeapBuffer_WriteRead(t *testing.T) {
	buf, err := offheap.NewBuffer(64 * 1024)
	require.NoError(t, err)

	require.NotNil(t, buf)
	defer buf.Release()

	n, err := buf.WriteString("GET /api/v1 HTTP/1.1\r\n\r\n")
	assert.NoError(t, err)
	assert.Equal(t, 24, n)
	assert.Equal(t, 24, buf.Len())

	bView := buf.Bytes()
	assert.Equal(t, "GET /api/v1 HTTP/1.1\r\n\r\n", string(bView))

	readDst := make([]byte, 64)
	rn, rErr := buf.Read(readDst)
	assert.NoError(t, rErr)
	assert.Equal(t, 24, rn)
	assert.Equal(t, "GET /api/v1 HTTP/1.1\r\n\r\n", string(readDst[:rn]))

	buf.Reset()
	assert.Equal(t, 0, buf.Len())
}

func TestBuffer_ReadCursor_Advances(t *testing.T) {
	buf, err := offheap.NewBuffer(64 * 1024)
	require.NoError(t, err)

	defer buf.Release()

	_, _ = buf.WriteString("ABCDEFGH")

	// First Read: should get "ABCD"
	p1 := make([]byte, 4)
	n, err := buf.Read(p1)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, "ABCD", string(p1))

	// Second Read: should get next "EFGH", not repeat "ABCD"
	p2 := make([]byte, 4)
	n, err = buf.Read(p2)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, "EFGH", string(p2))
}

func TestBuffer_ReadEOF(t *testing.T) {
	buf, err := offheap.NewBuffer(4096)
	require.NoError(t, err)

	defer buf.Release()

	_, _ = buf.WriteString("hello")

	p := make([]byte, 128)
	n, err := buf.Read(p)
	assert.NoError(t, err)
	assert.Equal(t, 5, n)

	// Next Read must return io.EOF
	n2, err2 := buf.Read(p)
	assert.Equal(t, 0, n2)
	assert.Equal(t, io.EOF, err2)
}

func TestBuffer_RewindRead(t *testing.T) {
	buf, err := offheap.NewBuffer(4096)
	require.NoError(t, err)

	defer buf.Release()

	_, _ = buf.WriteString("rewindable")

	p := make([]byte, 16)

	n, _ := buf.Read(p)
	assert.Equal(t, "rewindable", string(p[:n]))

	// After RewindRead the same data should be readable again
	buf.RewindRead()

	p2 := make([]byte, 16)
	n2, _ := buf.Read(p2)
	assert.Equal(t, "rewindable", string(p2[:n2]))

	// Len() must still reflect written bytes (unchanged)
	assert.Equal(t, len("rewindable"), buf.Len())
}

func TestBuffer_WriteString_NoAlloc(t *testing.T) {
	buf, err := offheap.NewBuffer(64 * 1024)
	require.NoError(t, err)

	defer buf.Release()

	allocs := testing.AllocsPerRun(1000, func() {
		buf.Reset()
		_, _ = buf.WriteString("content-type: application/json\r\n")
	})

	assert.Equal(t, float64(0), allocs, "WriteString must not allocate on the heap")
}

func TestBuffer_Full(t *testing.T) {
	buf, err := offheap.NewBuffer(8)
	require.NoError(t, err)

	defer buf.Release()

	_, err = buf.Write([]byte("12345678"))
	assert.NoError(t, err)
	assert.Equal(t, 8, buf.Len())

	_, err = buf.Write([]byte("X"))
	assert.ErrorIs(t, err, offheap.ErrBufferFull)
}

func TestBuffer_ClosedOps(t *testing.T) {
	buf, err := offheap.NewBuffer(4096)
	require.NoError(t, err)
	buf.Release()

	_, err = buf.Write([]byte("x"))
	assert.ErrorIs(t, err, offheap.ErrBufferClosed)

	_, err = buf.WriteString("x")
	assert.ErrorIs(t, err, offheap.ErrBufferClosed)

	_, err = buf.Read(make([]byte, 4))
	assert.ErrorIs(t, err, offheap.ErrBufferClosed)

	assert.Nil(t, buf.Bytes())
	assert.Equal(t, 0, buf.Len())
	assert.Equal(t, 0, buf.Cap())
}

// ── Arena ─────────────────────────────────────────────────────────────────────

func TestScopeRAII_AllocAndPanicResilience(t *testing.T) {
	scopeErr := offheap.Scope(2*1024*1024, func(arena *offheap.Arena) {
		require.NotNil(t, arena)

		b1 := arena.AllocBuffer(1024)
		require.NotNil(t, b1)

		_, wErr := b1.WriteString("hello offheap arena")
		assert.NoError(t, wErr)
		assert.Equal(t, "hello offheap arena", string(b1.Bytes()))
	})
	assert.NoError(t, scopeErr)

	// Verify panic resilience: Scope must release even when fn panics.
	defer func() {
		r := recover()
		assert.NotNil(t, r, "panic should propagate through Scope")
	}()

	_ = offheap.Scope(1024*1024, func(_ *offheap.Arena) {
		panic("testing panic resilience inside scope")
	})
}

func TestAllocStruct(t *testing.T) {
	err := offheap.Scope(64*1024, func(arena *offheap.Arena) {
		hdr := offheap.AllocStruct[testFrameHeader](arena)
		require.NotNil(t, hdr)

		hdr.StreamID = 100
		hdr.Length = 16384
		hdr.Flags = 0x1
		hdr.Type = 0x0

		assert.Equal(t, uint32(100), hdr.StreamID)
		assert.Equal(t, uint32(16384), hdr.Length)
		assert.Equal(t, uint8(0x1), hdr.Flags)
		assert.Equal(t, uint8(0x0), hdr.Type)

		slot := offheap.AllocStruct[testFastHeaderSlot](arena)
		require.NotNil(t, slot)

		copy(slot.Key[:], "content-type")
		copy(slot.Val[:], "application/json")
		slot.KeyLen = 12
		slot.ValLen = 16

		assert.Equal(t, uint8(12), slot.KeyLen)
		assert.Equal(t, uint8(16), slot.ValLen)
		assert.Equal(t, "content-type", string(slot.Key[:slot.KeyLen]))
		assert.Equal(t, "application/json", string(slot.Val[:slot.ValLen]))
	})
	assert.NoError(t, err)
}

func TestAllocStruct_AlignmentCorrectness(t *testing.T) {
	// AllocStruct must return naturally aligned pointers for all integer widths.
	type aligned8 struct{ X uint64 } // 8-byte alignment

	type aligned4 struct{ X uint32 } // 4-byte alignment

	type aligned2 struct{ X uint16 } // 2-byte alignment

	type aligned1 struct{ X uint8 } // 1-byte alignment

	type mixed struct { // mixed sizes: compiler pads
		A uint8
		B uint64
		C uint16
	}

	err := offheap.Scope(256*1024, func(a *offheap.Arena) {
		// Alternate small + large to provoke alignment padding
		for i := 0; i < 100; i++ {
			p1 := offheap.AllocStruct[aligned1](a)
			require.NotNil(t, p1)

			p8 := offheap.AllocStruct[aligned8](a)
			require.NotNil(t, p8)
			assert.Equal(t, uintptr(0), uintptr(unsafe.Pointer(p8))%8,
				"uint64 pointer must be 8-byte aligned")

			p4 := offheap.AllocStruct[aligned4](a)
			require.NotNil(t, p4)
			assert.Equal(t, uintptr(0), uintptr(unsafe.Pointer(p4))%4,
				"uint32 pointer must be 4-byte aligned")

			p2 := offheap.AllocStruct[aligned2](a)
			require.NotNil(t, p2)
			assert.Equal(t, uintptr(0), uintptr(unsafe.Pointer(p2))%2,
				"uint16 pointer must be 2-byte aligned")

			pm := offheap.AllocStruct[mixed](a)
			require.NotNil(t, pm)
			assert.Equal(t, uintptr(0), uintptr(unsafe.Pointer(pm))%unsafe.Alignof(*pm),
				"mixed struct pointer must satisfy its natural alignment")
		}
	})
	require.NoError(t, err)
}

func TestAllocStruct_AlignmentOverflow_NoCorruption(t *testing.T) {
	// Allocate a very small arena. Verify that alignment padding at exhaustion
	// causes graceful heap fallback without corrupting arena.offset > arena.size.
	type u64 struct{ X uint64 } // 8-byte alignment, 8-byte size

	// Arena of 9 bytes: fits one u64 (8 bytes), then 1 byte remains.
	// Next AllocStruct[u64] needs 7 bytes of padding (offset=8 → already aligned... let's use 11)
	// Arena of 11 bytes: first u64 at 0 (8 bytes). offset=8. Remaining=3.
	// Next u64 needs: rem = 8%8=0, no padding. Needs 8 bytes. 8>3 → fallback. ✓
	// Use 13 bytes: first u64 at 0. offset=8. Remaining=5.
	// Next u64: rem=8%8=0, no pad. Needs 8. 8>5 → Alloc returns nil → fallback. ✓
	// Use 15 bytes: offset=8. Remaining=7. Needs 8 → fallback. ✓

	arena, err := offheap.NewArena(15)
	require.NoError(t, err)

	defer arena.Release()

	p1 := offheap.AllocStruct[u64](arena)
	require.NotNil(t, p1, "first allocation must succeed")
	p1.X = 0xDEADBEEF_CAFEBABE

	// Second must fall back to heap — but must NOT panic or corrupt state
	p2 := offheap.AllocStruct[u64](arena)
	require.NotNil(t, p2, "fallback must return valid heap pointer")
	p2.X = 0x1234

	// First value must still be intact
	assert.Equal(t, uint64(0xDEADBEEF_CAFEBABE), p1.X, "off-heap data must not be corrupted")
}

func TestArena_ResetAndReuse(t *testing.T) {
	arena, err := offheap.NewArena(64 * 1024)
	require.NoError(t, err)

	defer arena.Release()

	for i := 0; i < 10; i++ {
		hdr := offheap.AllocStruct[testFrameHeader](arena)
		require.NotNil(t, hdr)
		hdr.StreamID = uint32(i * 100)

		arena.Reset()
	}

	// Must not panic or return nil after repeated Reset cycles.
}

// ── ArenaPool ─────────────────────────────────────────────────────────────────

func TestArenaPool_BasicAcquireRelease(t *testing.T) {
	pool := offheap.NewArenaPool(1024 * 1024)

	a := pool.Acquire()
	require.NotNil(t, a)

	hdr := offheap.AllocStruct[testFrameHeader](a)
	require.NotNil(t, hdr)
	hdr.StreamID = 999

	pool.Release(a)

	// Re-acquire: arena must be reset (offset 0), but memory valid.
	a2 := pool.Acquire()
	require.NotNil(t, a2)
	pool.Release(a2)
}

func TestArenaPool_PageSize(t *testing.T) {
	pool := offheap.NewArenaPool(512 * 1024)
	assert.Equal(t, 512*1024, pool.PageSize())
}

func TestArenaPool_DefaultPageSize(t *testing.T) {
	pool := offheap.NewArenaPool(0)
	assert.Equal(t, 2*1024*1024, pool.PageSize())
}

func TestArenaPool_Concurrent(t *testing.T) {
	pool := offheap.NewArenaPool(512 * 1024)

	const (
		goroutines = 64
		iters      = 200
	)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()

			for i := 0; i < iters; i++ {
				a := pool.Acquire()
				if a == nil {
					continue
				}

				hdr := offheap.AllocStruct[testFrameHeader](a)
				if hdr != nil {
					hdr.StreamID = uint32(i)
				}

				pool.Release(a)
			}
		}()
	}

	wg.Wait()
}

// ── Fuzz ──────────────────────────────────────────────────────────────────────

func FuzzArena_AllocStruct(f *testing.F) {
	f.Add(1024, 8)
	f.Add(64, 1)
	f.Add(1<<20, 500)
	f.Add(1, 1)

	f.Fuzz(func(t *testing.T, arenaSize, allocCount int) {
		if arenaSize <= 0 || arenaSize > 32*1024*1024 {
			return
		}

		if allocCount < 0 || allocCount > 50_000 {
			return
		}

		arena, err := offheap.NewArena(arenaSize)
		if err != nil {
			return
		}
		defer arena.Release()

		for i := 0; i < allocCount; i++ {
			hdr := offheap.AllocStruct[testFrameHeader](arena)
			if hdr == nil {
				// Arena exhausted, fell back to heap — expected.
				break
			}

			hdr.StreamID = uint32(i)
		}

		// Must not panic after exhaustion + Reset cycle.
		arena.Reset()

		hdr := offheap.AllocStruct[testFrameHeader](arena)
		if hdr != nil {
			hdr.StreamID = 42
		}
	})
}
