// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"bytes"
	"net"
	"testing"

	"github.com/lemon4ksan/aoni/foundation/offheap"
)

func buildBenchPayload() []byte {
	var (
		buf bytes.Buffer
		tmp [8]byte
	)

	for i := range 16 {
		n := EncodeVarint(uint64(i+1), tmp[:])
		buf.Write(tmp[:n])
		buf.WriteByte(4)
		buf.Write(net.ParseIP("10.0.0.1").To4())
		buf.WriteByte(24)
	}

	return buf.Bytes()
}

func BenchmarkDecodeAddressAssign_Heap(b *testing.B) {
	payload := buildBenchPayload()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		entries, _ := DecodeAddressAssignPayloadPOD(nil, payload)
		_ = entries
	}
}

func BenchmarkDecodeAddressAssign_Arena(b *testing.B) {
	payload := buildBenchPayload()

	arena, err := offheap.NewArena(1 << 20)
	if err != nil {
		b.Fatal(err)
	}

	defer arena.Release()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		arena.Reset()
		entries, _ := DecodeAddressAssignPayloadPOD(arena, payload)
		_ = entries
	}
}

func BenchmarkDecodeAddressAssign_Slab(b *testing.B) {
	payload := buildBenchPayload()

	slab, err := NewAssignedAddressSlab(64)
	if err != nil {
		b.Fatal(err)
	}

	defer slab.Release()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		entries, _ := DecodeAddressAssignPayloadPODSlab(slab, payload)

		for _, e := range entries {
			slab.Free(e)
		}
	}
}
