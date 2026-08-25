// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"bytes"
	"net"
	"net/netip"
	"testing"

	"github.com/lemon4ksan/foundation/silicon/offheap"
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

func BenchmarkEncodeAddressRequestCapsule(b *testing.B) {
	reqs := []RequestedAddress{
		{
			Addr:         netip.MustParseAddr("10.0.0.1"),
			RequestID:    1,
			IPVersion:    4,
			PrefixLength: 32,
		},
		{
			Addr:         netip.MustParseAddr("2001:db8::1"),
			RequestID:    2,
			IPVersion:    6,
			PrefixLength: 64,
		},
	}

	var buf [512]byte

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = EncodeAddressRequestCapsule(reqs, buf[:])
	}
}

func BenchmarkDecodeAddressRequestPayloadTo(b *testing.B) {
	reqs := []RequestedAddress{
		{
			Addr:         netip.MustParseAddr("10.0.0.1"),
			RequestID:    1,
			IPVersion:    4,
			PrefixLength: 32,
		},
		{
			Addr:         netip.MustParseAddr("2001:db8::1"),
			RequestID:    2,
			IPVersion:    6,
			PrefixLength: 64,
		},
	}

	var payloadBuf [512]byte

	payloadLen, _ := EncodeAddressRequestPayload(reqs, payloadBuf[:])
	payload := payloadBuf[:payloadLen]

	dst := make([]RequestedAddress, 0, 4)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		dst = dst[:0]
		_, _ = DecodeAddressRequestPayloadTo(payload, dst)
	}
}

func BenchmarkEncodeRouteAdvertisementCapsule(b *testing.B) {
	routes := []IPAddressRange{
		{
			StartIP:    netip.MustParseAddr("192.168.1.0"),
			EndIP:      netip.MustParseAddr("192.168.1.255"),
			IPVersion:  4,
			IPProtocol: 6,
		},
		{
			StartIP:    netip.MustParseAddr("2001:db8::1"),
			EndIP:      netip.MustParseAddr("2001:db8::ffff"),
			IPVersion:  6,
			IPProtocol: 17,
		},
	}

	var buf [512]byte

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = EncodeRouteAdvertisementCapsule(routes, buf[:])
	}
}

func BenchmarkDecodeRouteAdvertisementPayloadTo(b *testing.B) {
	routes := []IPAddressRange{
		{
			StartIP:    netip.MustParseAddr("192.168.1.0"),
			EndIP:      netip.MustParseAddr("192.168.1.255"),
			IPVersion:  4,
			IPProtocol: 6,
		},
		{
			StartIP:    netip.MustParseAddr("2001:db8::1"),
			EndIP:      netip.MustParseAddr("2001:db8::ffff"),
			IPVersion:  6,
			IPProtocol: 17,
		},
	}

	var payloadBuf [512]byte

	payloadLen, _ := EncodeRouteAdvertisementPayload(routes, payloadBuf[:])
	payload := payloadBuf[:payloadLen]

	dst := make([]IPAddressRange, 0, 4)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		dst = dst[:0]
		_, _ = DecodeRouteAdvertisementPayloadTo(payload, dst)
	}
}
