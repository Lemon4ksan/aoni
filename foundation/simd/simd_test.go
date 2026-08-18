// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package simd_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/foundation/simd"
)

func TestIndexByteSWAR(t *testing.T) {
	data := []byte("Host: api.example.com\r\nAccept: application/json\r\n")

	colonIdx := simd.IndexByteSWAR(data, ':')
	assert.Equal(t, 4, colonIdx)

	newlineIdx := simd.IndexByteSWAR(data, '\n')
	assert.Equal(t, 22, newlineIdx)

	missingIdx := simd.IndexByteSWAR(data, 'Z')
	assert.Equal(t, -1, missingIdx)
}

func TestIndexByteTwoSWAR(t *testing.T) {
	data := []byte("User-Agent: Mozilla/5.0; Chrome/120")

	idx := simd.IndexByteTwoSWAR(data, ';', ':')
	assert.Equal(t, 10, idx)
}

func TestIndexByteVector(t *testing.T) {
	data := []byte("Host: api.example.com\r\nAccept: application/json\r\nUser-Agent: aoni-fast-client/1.0\r\n")

	colonIdx := simd.IndexByteVector(data, ':')
	assert.Equal(t, 4, colonIdx)

	newlineIdx := simd.IndexByteVector(data, '\n')
	assert.Equal(t, 22, newlineIdx)

	missingIdx := simd.IndexByteVector(data, 'Z')
	assert.Equal(t, -1, missingIdx)
}

func TestIndexTwoBytesVector(t *testing.T) {
	data := []byte("Host: api.example.com\r\nAccept: application/json\r\nUser-Agent: aoni-fast-client/1.0\r\n")

	idx := simd.IndexTwoBytesVector(data, ':', '\n')
	assert.Equal(t, 4, idx)
}

func TestApplyFastMaskVector(t *testing.T) {
	payload := []byte("Hello WebSocket World! 123456789012345678901234567890")
	orig := make([]byte, len(payload))
	copy(orig, payload)

	mask := uint32(0x12345678)
	simd.ApplyFastMaskVector(payload, mask)
	assert.NotEqual(t, orig, payload)

	// Unmasking
	simd.ApplyFastMaskVector(payload, mask)
	assert.Equal(t, orig, payload)
}

func BenchmarkIndexByte_Std(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = 'a'
	}

	data[1023] = 'z'

	b.ReportAllocs()

	for b.Loop() {
		_ = bytes.IndexByte(data, 'z')
	}
}

func BenchmarkIndexByte_SWAR(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = 'a'
	}

	data[1023] = 'z'

	b.ReportAllocs()

	for b.Loop() {
		_ = simd.IndexByteSWAR(data, 'z')
	}
}

func BenchmarkIndexByte_AVX2(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = 'a'
	}

	data[1023] = 'z'

	b.ReportAllocs()

	for b.Loop() {
		_ = simd.IndexByteVector(data, 'z')
	}
}

func BenchmarkIndexTwoBytes_SWAR(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = 'a'
	}

	data[1023] = '\n'

	b.ReportAllocs()

	for b.Loop() {
		_ = simd.IndexByteTwoSWAR(data, '\r', '\n')
	}
}

func BenchmarkIndexTwoBytes_AVX2(b *testing.B) {
	data := make([]byte, 1024)
	for i := range data {
		data[i] = 'a'
	}

	data[1023] = '\n'

	b.ReportAllocs()

	for b.Loop() {
		_ = simd.IndexTwoBytesVector(data, '\r', '\n')
	}
}
