// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package offheap_test

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/offheap"
)

// ── SlabAllocator ────────────────────────────────────────────────────────────

func TestSlabAllocator_BasicAllocFree(t *testing.T) {
	slab, err := offheap.NewSlabAllocator[testFrameHeader](64)
	require.NoError(t, err)

	require.NotNil(t, slab)
	defer slab.Release()

	assert.Equal(t, 64, slab.Cap())
	assert.Equal(t, 64, slab.Available())
	assert.Equal(t, 0, slab.Len())

	hdr := slab.Alloc()
	require.NotNil(t, hdr)

	assert.Equal(t, 63, slab.Available())
	assert.Equal(t, 1, slab.Len())

	// Slot must be zero-initialized.
	assert.Equal(t, uint32(0), hdr.StreamID)

	hdr.StreamID = 0xDEAD
	slab.Free(hdr)

	assert.Equal(t, 64, slab.Available())
	assert.Equal(t, 0, slab.Len())
}

func TestSlabAllocator_FreeAndReallocate(t *testing.T) {
	slab, err := offheap.NewSlabAllocator[testFrameHeader](4)
	require.NoError(t, err)

	defer slab.Release()

	p0 := slab.Alloc()
	p1 := slab.Alloc()
	p2 := slab.Alloc()

	require.NotNil(t, p0)
	require.NotNil(t, p1)
	require.NotNil(t, p2)

	p0.StreamID = 111
	p1.StreamID = 222
	p2.StreamID = 333

	// Free p1 in the middle.
	slab.Free(p1)
	assert.Equal(t, 2, slab.Len())

	// Next alloc should reuse p1's slot (first free bit found).
	p3 := slab.Alloc()
	require.NotNil(t, p3)

	// The reallocated slot must be zero-initialized.
	assert.Equal(t, uint32(0), p3.StreamID)

	// p0 and p2 data must be intact.
	assert.Equal(t, uint32(111), p0.StreamID)
	assert.Equal(t, uint32(333), p2.StreamID)
}

func TestSlabAllocator_FullCapacity_ReturnsNil(t *testing.T) {
	const cap = 63 // Tests non-power-of-two (crosses a partial bitmap word)

	slab, err := offheap.NewSlabAllocator[testFrameHeader](cap)
	require.NoError(t, err)

	defer slab.Release()

	ptrs := make([]*testFrameHeader, 0, cap)

	for i := range cap {
		p := slab.Alloc()
		require.NotNil(t, p, "allocation %d must succeed", i)
		p.StreamID = uint32(i)
		ptrs = append(ptrs, p)
	}

	assert.Equal(t, 0, slab.Available())

	// One more alloc must return nil.
	overflow := slab.Alloc()
	assert.Nil(t, overflow, "slab must return nil when full")

	// All previously allocated pointers must be intact.
	for i, p := range ptrs {
		assert.Equal(t, uint32(i), p.StreamID)
	}
}

func TestSlabAllocator_Reset(t *testing.T) {
	slab, err := offheap.NewSlabAllocator[testFrameHeader](32)
	require.NoError(t, err)

	defer slab.Release()

	// Fill completely.
	for range 32 {
		p := slab.Alloc()
		require.NotNil(t, p)
	}

	assert.Equal(t, 0, slab.Available())

	slab.Reset()

	assert.Equal(t, 32, slab.Available())
	assert.Equal(t, 0, slab.Len())

	// Must be allocatable again after Reset.
	p := slab.Alloc()
	require.NotNil(t, p)
	assert.Equal(t, 31, slab.Available())
}

func TestSlabAllocator_AlignmentCorrectness(t *testing.T) {
	// All allocated pointers must satisfy natural alignment of T.
	slab, err := offheap.NewSlabAllocator[testFastHeaderSlot](128)
	require.NoError(t, err)

	defer slab.Release()

	for i := range 128 {
		p := slab.Alloc()
		require.NotNil(t, p, "slot %d must be allocatable", i)

		align := unsafe.Alignof(*p)
		addr := uintptr(unsafe.Pointer(p))
		assert.Equal(t, uintptr(0), addr%align, "slot %d must be naturally aligned", i)
	}
}

func TestSlabAllocator_InvalidFree_Ignored(t *testing.T) {
	slab, err := offheap.NewSlabAllocator[testFrameHeader](8)
	require.NoError(t, err)

	defer slab.Release()

	// Freeing a Go heap pointer not from this slab must not panic.
	heapObj := &testFrameHeader{StreamID: 42}
	assert.NotPanics(t, func() { slab.Free(heapObj) })

	// Freeing nil must not panic.
	assert.NotPanics(t, func() { slab.Free(nil) })

	// Stats must be unchanged.
	assert.Equal(t, 8, slab.Available())
}

func TestSlabAllocator_ZeroInit_AfterFree(t *testing.T) {
	slab, err := offheap.NewSlabAllocator[testFrameHeader](4)
	require.NoError(t, err)

	defer slab.Release()

	p := slab.Alloc()
	require.NotNil(t, p)

	p.StreamID = 0xCAFE
	p.Length = 0xBEEF

	slab.Free(p)

	// After Free, the memory must be zeroed — verified by re-allocating the slot.
	p2 := slab.Alloc()
	require.NotNil(t, p2)
	assert.Equal(t, uint32(0), p2.StreamID)
	assert.Equal(t, uint32(0), p2.Length)
}

func TestSlabAllocator_InvalidArgs(t *testing.T) {
	_, err := offheap.NewSlabAllocator[testFrameHeader](0)
	assert.Error(t, err)

	_, err = offheap.NewSlabAllocator[testFrameHeader](-1)
	assert.Error(t, err)
}

// ── CastBytes ────────────────────────────────────────────────────────────────

func TestCastBytes_Float32(t *testing.T) {
	// Build a []byte representation of 4 float32 values.
	orig := []float32{1.0, 2.5, -3.14, 0.0}
	buf := make([]byte, len(orig)*4)

	for i, v := range orig {
		bits := *(*uint32)(unsafe.Pointer(&v))
		binary.LittleEndian.PutUint32(buf[i*4:], bits)
	}

	cast := offheap.CastBytes[float32](buf)
	require.Len(t, cast, 4)

	for i, v := range orig {
		assert.Equal(t, v, cast[i], "element %d mismatch", i)
	}
}

func TestCastBytes_PODStruct(t *testing.T) {
	slab, err := offheap.NewSlabAllocator[testFrameHeader](4)
	require.NoError(t, err)

	defer slab.Release()

	hdr := slab.Alloc()
	require.NotNil(t, hdr)

	hdr.StreamID = 7
	hdr.Length = 1024

	b := offheap.StructBytes(hdr)
	require.Equal(t, int(unsafe.Sizeof(*hdr)), len(b))

	// CastBytes on the struct bytes must produce the same header.
	headers := offheap.CastBytes[testFrameHeader](b)
	require.Len(t, headers, 1)
	assert.Equal(t, uint32(7), headers[0].StreamID)
	assert.Equal(t, uint32(1024), headers[0].Length)
}

func TestCastBytes_EmptySlice(t *testing.T) {
	result := offheap.CastBytes[testFrameHeader](nil)
	assert.Nil(t, result)

	result2 := offheap.CastBytes[testFrameHeader]([]byte{})
	assert.Nil(t, result2)
}

// ── WriteStruct ───────────────────────────────────────────────────────────────

func TestWriteStruct_RoundTrip(t *testing.T) {
	buf, err := offheap.NewBuffer(4096)
	require.NoError(t, err)

	defer buf.Release()

	hdr := &testFrameHeader{
		StreamID: 99,
		Length:   16384,
		Flags:    0x5,
		Type:     0x1,
	}

	n, err := offheap.WriteStruct(buf, hdr)
	require.NoError(t, err)
	assert.Equal(t, int(unsafe.Sizeof(*hdr)), n)

	// The bytes in the buffer must equal the raw memory of hdr.
	raw := offheap.StructBytes(hdr)
	assert.Equal(t, raw, buf.Bytes())
}

func TestWriteStruct_NoAlloc(t *testing.T) {
	buf, err := offheap.NewBuffer(4096)
	require.NoError(t, err)

	defer buf.Release()

	hdr := &testFrameHeader{StreamID: 1}

	allocs := testing.AllocsPerRun(1000, func() {
		buf.Reset()
		_, _ = offheap.WriteStruct(buf, hdr)
	})

	assert.Equal(t, float64(0), allocs, "WriteStruct must be allocation-free")
}

func TestWriteStruct_NilStruct(t *testing.T) {
	buf, err := offheap.NewBuffer(4096)
	require.NoError(t, err)

	defer buf.Release()

	n, err := offheap.WriteStruct[testFrameHeader](buf, nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, n)
}
