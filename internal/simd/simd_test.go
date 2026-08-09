// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package simd_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/simd"
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
