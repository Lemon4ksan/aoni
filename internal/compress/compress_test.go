// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package compress_test

import (
	"bytes"
	stdgzip "compress/gzip"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/compress"
	"github.com/lemon4ksan/aoni/internal/compress/flate"
	"github.com/lemon4ksan/aoni/internal/compress/gzip"
)

func createGzipData(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := stdgzip.NewWriter(&buf)
	_, err := w.Write(payload)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func createDeflateData(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	require.NoError(t, err)
	_, err = w.Write(payload)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func createZstdRawBlock(payload []byte) []byte {
	// Frame Header with SingleSegment=true, ContentSize=len(payload)
	// Magic: 0x28, 0xb5, 0x2f, 0xfd
	// FHD: 0x20 (Single_Segment=1)
	// FCS: len(payload) as 1 byte (if < 256)
	// Raw Block Header: last_block=1 (bit 0), block_type=raw (bits 1-2 = 0), block_size = len(payload) (bits 3-23)
	// Block Size Header = 1 | (len(payload) << 3) as 3 bytes LE
	var buf bytes.Buffer
	buf.Write([]byte{0x28, 0xb5, 0x2f, 0xfd, 0x20, byte(len(payload))})
	bh := uint32(1) | (uint32(len(payload)) << 3)
	buf.Write([]byte{byte(bh), byte(bh >> 8), byte(bh >> 16)})
	buf.Write(payload)
	return buf.Bytes()
}

func TestGunzip(t *testing.T) {
	t.Parallel()

	original := []byte("Hello, world! This is a test of high-speed gzip decompression in internal/compress.")
	compressed := createGzipData(t, original)

	decompressed, err := compress.Gunzip(compressed, nil)
	require.NoError(t, err)
	assert.Equal(t, original, decompressed)

	// Test Decompress dispatcher
	viaDispatcher, err := compress.Decompress("gzip", compressed, nil)
	require.NoError(t, err)
	assert.Equal(t, original, viaDispatcher)
}

func TestInflate(t *testing.T) {
	t.Parallel()

	original := []byte("Testing raw deflate RFC 1951 decompression.")
	compressed := createDeflateData(t, original)

	decompressed, err := compress.Inflate(compressed, nil)
	require.NoError(t, err)
	assert.Equal(t, original, decompressed)

	viaDispatcher, err := compress.Decompress("deflate", compressed, nil)
	require.NoError(t, err)
	assert.Equal(t, original, viaDispatcher)
}

func TestUnzstd(t *testing.T) {
	t.Parallel()

	original := []byte("Zstandard decompression test payload in aoni internal/compress.")
	compressed := createZstdRawBlock(original)

	decompressed, err := compress.Unzstd(compressed, nil)
	require.NoError(t, err)
	assert.Equal(t, original, decompressed)

	viaDispatcher, err := compress.Decompress("zstd", compressed, nil)
	require.NoError(t, err)
	assert.Equal(t, original, viaDispatcher)

	// Test streaming zstd reader
	zr, err := compress.AcquireZstdReader(bytes.NewReader(compressed))
	require.NoError(t, err)
	defer compress.ReleaseZstdReader(zr)

	buf, err := io.ReadAll(zr)
	require.NoError(t, err)
	assert.Equal(t, original, buf)
}

func TestStreamingReaders(t *testing.T) {
	t.Parallel()

	original := []byte("Streaming compression test data.")
	compressed := createGzipData(t, original)

	zr, err := compress.AcquireGzipReader(bytes.NewReader(compressed))
	require.NoError(t, err)
	defer compress.ReleaseGzipReader(zr)

	readBuf, err := io.ReadAll(zr)
	require.NoError(t, err)
	assert.Equal(t, original, readBuf)
}

func TestIsCompressed(t *testing.T) {
	t.Parallel()

	gzipData := []byte{0x1f, 0x8b, 0x08, 0x00}
	zstdData := []byte{0x28, 0xb5, 0x2f, 0xfd}
	plainData := []byte("plain text content")

	assert.True(t, compress.IsCompressed(gzipData))
	assert.True(t, compress.IsCompressed(zstdData))
	assert.False(t, compress.IsCompressed(plainData))
	assert.False(t, compress.IsCompressed([]byte{0x01}))
}

func TestMatchesEncoding(t *testing.T) {
	t.Parallel()

	assert.True(t, compress.MatchesEncoding([]byte("gzip, deflate, br"), "gzip"))
	assert.True(t, compress.MatchesEncoding([]byte("gzip, deflate, br"), "br"))
	assert.True(t, compress.MatchesEncoding([]byte("zstd, gzip"), "zstd"))
	assert.False(t, compress.MatchesEncoding([]byte("deflate"), "zstd"))
}

func BenchmarkGunzip(b *testing.B) {
	payload := []byte(strings.Repeat("Zero allocation decompression benchmark for aoni internal/compress. ", 50))
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write(payload)
	_ = w.Close()
	compressed := buf.Bytes()
	dst := make([]byte, 0, len(payload))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = compress.Gunzip(compressed, dst)
	}
}

func BenchmarkUnzstd(b *testing.B) {
	payload := []byte(strings.Repeat("Zstd benchmark payload for internal/compress decoder. ", 50))
	compressed := createZstdRawBlock(payload)
	dst := make([]byte, 0, len(payload))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = compress.Unzstd(compressed, dst)
	}
}
