// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package qpack

import (
	"testing"
)

func BenchmarkQPACK_AppendInt(b *testing.B) {
	dst := make([]byte, 1, 16)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		dst[0] = 0x80
		dst = appendInt(dst[:1], 6, 12345678)
	}

	_ = dst
}

func BenchmarkQPACK_ReadInt(b *testing.B) {
	data := []byte{0xbf, 0x9e, 0xa4, 0x05}

	b.ReportAllocs()
	b.ResetTimer()

	var total uint64
	for i := 0; i < b.N; i++ {
		val, _, _ := readInt(6, data)
		total += val
	}

	_ = total
}
