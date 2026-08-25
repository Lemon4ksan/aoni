// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ws

import (
	"testing"
)

func BenchmarkWS_ApplyMask_64B(b *testing.B) {
	buf := make([]byte, 64)
	mask := [4]byte{0x12, 0x34, 0x56, 0x78}
	b.SetBytes(int64(len(buf)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		applyFastMask(buf, mask)
	}
}

func BenchmarkWS_ApplyMask_1KB(b *testing.B) {
	buf := make([]byte, 1024)
	mask := [4]byte{0x12, 0x34, 0x56, 0x78}
	b.SetBytes(int64(len(buf)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		applyFastMask(buf, mask)
	}
}

func BenchmarkWS_ApplyMask_64KB(b *testing.B) {
	buf := make([]byte, 64*1024)
	mask := [4]byte{0x12, 0x34, 0x56, 0x78}
	b.SetBytes(int64(len(buf)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		applyFastMask(buf, mask)
	}
}

func BenchmarkWS_BuildFrameHeader(b *testing.B) {
	var hdr [10]byte
	conn := &wsRawConn{isClient: true}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = conn.buildFrameHeaderZeroAlloc(0x01, 1024, false, hdr[:])
	}
}
