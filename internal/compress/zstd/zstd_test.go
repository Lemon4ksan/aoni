// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zstd_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/compress/zstd"
)

func createZstdRawBlock(payload []byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x28, 0xb5, 0x2f, 0xfd, 0x20, byte(len(payload))})
	bh := uint32(1) | (uint32(len(payload)) << 3)
	buf.Write([]byte{byte(bh), byte(bh >> 8), byte(bh >> 16)})
	buf.Write(payload)

	return buf.Bytes()
}

func TestZstdDecoder(t *testing.T) {
	t.Parallel()

	data := []byte("Zstandard standalone decoder unit test payload in internal/compress/zstd.")
	compressed := createZstdRawBlock(data)

	dec, err := zstd.NewReader(bytes.NewReader(compressed), zstd.WithDecoderConcurrency(1))
	require.NoError(t, err)
	defer dec.Close()

	decompressed, err := io.ReadAll(dec)
	require.NoError(t, err)
	assert.Equal(t, data, decompressed)

	// Test DecodeAll
	allDecompressed, err := dec.DecodeAll(compressed, nil)
	require.NoError(t, err)
	assert.Equal(t, data, allDecompressed)
}
