// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protocol_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
)

func BenchmarkDecodePacketNumber(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	var total protocol.PacketNumber
	for i := 0; i < b.N; i++ {
		pn := protocol.DecodePacketNumber(protocol.PacketNumberLen2, 0xa82f30ea, 0x9b32)
		total += pn
	}

	_ = total
}

func BenchmarkPacketNumberLengthForHeader(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	var total protocol.PacketNumberLen
	for i := 0; i < b.N; i++ {
		l := protocol.PacketNumberLengthForHeader(0xace8fe, 0xabe8b3)
		total += l
	}

	_ = total
}
