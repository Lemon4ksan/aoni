// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package offheap_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/offheap"
)

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

	buf.Reset()
	assert.Equal(t, 0, buf.Len())
}

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

	// Verify panic resilience
	defer func() {
		r := recover()
		assert.NotNil(t, r, "panic should be recovered")
	}()

	_ = offheap.Scope(1024*1024, func(_ *offheap.Arena) {
		panic("testing panic resilience inside scope")
	})
}

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

var testPool = sync.Pool{
	New: func() any { return new(testFrameHeader) },
}

var benchSum uint64

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
		hdr := testPool.Get().(*testFrameHeader)
		hdr.StreamID = uint32(i)
		sum += uint64(hdr.StreamID)
		testPool.Put(hdr)
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
