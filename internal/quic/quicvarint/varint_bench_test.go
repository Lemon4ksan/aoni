// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quicvarint_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

func BenchmarkVarint_Parse_1Byte(b *testing.B) {
	buf := []byte{25}
	b.ReportAllocs()

	var total uint64
	for b.Loop() {
		val, _, _ := quicvarint.Parse(buf)
		total += val
	}

	_ = total
}

func BenchmarkVarint_Parse_2Byte(b *testing.B) {
	buf := []byte{0x40 | 0x01, 0x23}
	b.ReportAllocs()

	var total uint64
	for b.Loop() {
		val, _, _ := quicvarint.Parse(buf)
		total += val
	}

	_ = total
}

func BenchmarkVarint_Parse_4Byte(b *testing.B) {
	buf := []byte{0x80 | 0x12, 0x34, 0x56, 0x78}
	b.ReportAllocs()

	var total uint64
	for b.Loop() {
		val, _, _ := quicvarint.Parse(buf)
		total += val
	}

	_ = total
}

func BenchmarkVarint_Parse_8Byte(b *testing.B) {
	buf := []byte{0xc0 | 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	b.ReportAllocs()

	var total uint64
	for b.Loop() {
		val, _, _ := quicvarint.Parse(buf)
		total += val
	}

	_ = total
}

func BenchmarkVarint_Append_4Byte(b *testing.B) {
	buf := make([]byte, 0, 8)
	val := uint64(123456789)
	b.ReportAllocs()

	for b.Loop() {
		buf = quicvarint.Append(buf[:0], val)
	}

	_ = buf
}

func BenchmarkVarint_Append_8Byte(b *testing.B) {
	buf := make([]byte, 0, 8)
	val := uint64(1234567890123456789)
	b.ReportAllocs()

	for b.Loop() {
		buf = quicvarint.Append(buf[:0], val)
	}

	_ = buf
}

func BenchmarkVarint_Len(b *testing.B) {
	val := uint64(1234567890123456789)
	b.ReportAllocs()

	var total int
	for b.Loop() {
		total += quicvarint.Len(val)
	}

	_ = total
}
