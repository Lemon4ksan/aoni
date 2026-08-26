// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h2engine_test

import (
	"testing"

	"github.com/lemon4ksan/aoni/internal/fast/h2engine"
)

func BenchmarkHuffman_Encode_Short(b *testing.B) {
	src := []byte("www.example.com")
	dst := make([]byte, 0, 64)

	b.ReportAllocs()

	for b.Loop() {
		dst = h2engine.HuffmanEncode(dst[:0], src)
	}

	_ = dst
}

func BenchmarkHuffman_Encode_Long(b *testing.B) {
	src := []byte(
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	)
	dst := make([]byte, 0, 256)

	b.ReportAllocs()

	for b.Loop() {
		dst = h2engine.HuffmanEncode(dst[:0], src)
	}

	_ = dst
}

func BenchmarkHuffman_Decode_Short(b *testing.B) {
	src := h2engine.HuffmanEncode(nil, []byte("www.example.com"))
	dst := make([]byte, 0, 64)

	b.ReportAllocs()

	for b.Loop() {
		dst = h2engine.HuffmanDecode(dst[:0], src)
	}

	_ = dst
}

func BenchmarkHuffman_Decode_Long(b *testing.B) {
	raw := []byte(
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	)
	src := h2engine.HuffmanEncode(nil, raw)
	dst := make([]byte, 0, 256)

	b.ReportAllocs()

	for b.Loop() {
		dst = h2engine.HuffmanDecode(dst[:0], src)
	}

	_ = dst
}

func BenchmarkHuffman_EncodeLength(b *testing.B) {
	src := []byte(
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	)

	b.ReportAllocs()

	var total int
	for b.Loop() {
		total += h2engine.HuffmanEncodeLength(src)
	}

	_ = total
}
