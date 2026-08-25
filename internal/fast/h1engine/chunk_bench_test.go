// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1engine

import (
	"bufio"
	"bytes"
	"testing"
)

func BenchmarkH1_ParseHexUint(b *testing.B) {
	src := []byte("1a4f")
	b.ReportAllocs()
	b.ResetTimer()

	var total int
	for i := 0; i < b.N; i++ {
		val, _, _ := vectorParseHexUint(src)
		total += val
	}
	_ = total
}

func BenchmarkH1_FormatHexUint(b *testing.B) {
	var buf [16]byte
	b.ReportAllocs()
	b.ResetTimer()

	var total int
	for i := 0; i < b.N; i++ {
		n := vectorFormatHexUint(&buf, 6725)
		total += n
	}
	_ = total
}

func BenchmarkH1_WriteHexInt(b *testing.B) {
	var out bytes.Buffer
	w := bufio.NewWriter(&out)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		out.Reset()
		_ = writeHexInt(w, 6725)
	}
}
