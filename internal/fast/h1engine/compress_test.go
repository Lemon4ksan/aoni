// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package h1engine_test

import (
	"bytes"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/foundation/codec/compress"

	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
)

func TestH1Engine_Compression_Gzip(t *testing.T) {
	t.Parallel()

	payload := []byte("Hello Gzip Compression via foundation compress engine! 1234567890")

	// 1. AppendGzipBytes / AppendGunzipBytes
	compressed := h1engine.AppendGzipBytes(nil, payload)
	require.NotEmpty(t, compressed)

	uncompressed, err := h1engine.AppendGunzipBytes(nil, compressed)
	require.NoError(t, err)
	assert.Equal(t, payload, uncompressed)

	// 2. Stream writers
	var buf bytes.Buffer
	n, err := h1engine.WriteGzip(&buf, payload)
	require.NoError(t, err)
	assert.Equal(t, len(payload), n)

	var out bytes.Buffer
	_, err = h1engine.WriteGunzip(&out, buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, payload, out.Bytes())
}

func TestH1Engine_Compression_Brotli(t *testing.T) {
	t.Parallel()

	payload := []byte("Hello Brotli Compression via foundation compress engine! ABCDEFGHIJKLMNOPQRSTUVWXYZ")

	// 1. AppendBrotliBytes / AppendUnbrotliBytes
	compressed := h1engine.AppendBrotliBytes(nil, payload)
	require.NotEmpty(t, compressed)

	uncompressed, err := h1engine.AppendUnbrotliBytes(nil, compressed)
	require.NoError(t, err)
	assert.Equal(t, payload, uncompressed)

	// 2. Stream writers
	var buf bytes.Buffer
	n, err := h1engine.WriteBrotli(&buf, payload)
	require.NoError(t, err)
	assert.True(t, n > 0)

	var out bytes.Buffer
	_, err = h1engine.WriteUnbrotli(&out, buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, payload, out.Bytes())
}

func TestH1Engine_Compression_Zstd(t *testing.T) {
	t.Parallel()

	payload := []byte("Hello Zstandard Compression via foundation compress engine! 9876543210")

	// Compress using foundation CompressZstd to test unzstd
	compressed, err := compress.CompressZstd(payload, nil)
	require.NoError(t, err)

	uncompressed, err := h1engine.AppendUnzstdBytes(nil, compressed)
	require.NoError(t, err)
	assert.Equal(t, payload, uncompressed)

	var out bytes.Buffer
	_, err = h1engine.WriteUnzstd(&out, compressed)
	require.NoError(t, err)
	assert.Equal(t, payload, out.Bytes())
}

func TestH1Engine_Compression_Deflate(t *testing.T) {
	t.Parallel()

	payload := []byte("Hello Deflate Compression via foundation compress engine! @#$%^&*()")

	// 1. AppendDeflateBytes / AppendInflateBytes
	compressed := h1engine.AppendDeflateBytes(nil, payload)
	require.NotEmpty(t, compressed)

	var out bytes.Buffer
	_, err := h1engine.WriteInflate(&out, compressed)
	require.NoError(t, err)
	assert.Equal(t, payload, out.Bytes())
}

func TestH1Engine_Response_BodyUncompressed(t *testing.T) {
	t.Parallel()

	raw := []byte("Response uncompressed payload testing across all algorithms")

	tests := []struct {
		name     string
		encoding string
		compress func(src []byte) []byte
	}{
		{
			name:     "gzip",
			encoding: "gzip",
			compress: func(src []byte) []byte {
				return h1engine.AppendGzipBytes(nil, src)
			},
		},
		{
			name:     "br",
			encoding: "br",
			compress: func(src []byte) []byte {
				return h1engine.AppendBrotliBytes(nil, src)
			},
		},
		{
			name:     "zstd",
			encoding: "zstd",
			compress: func(src []byte) []byte {
				res, _ := compress.CompressZstd(src, nil)
				return res
			},
		},
		{
			name:     "identity",
			encoding: "identity",
			compress: func(src []byte) []byte {
				return src
			},
		},
		{
			name:     "empty",
			encoding: "",
			compress: func(src []byte) []byte {
				return src
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var resp h1engine.Response
			resp.Header.Set("Content-Encoding", tc.encoding)
			resp.SetBody(tc.compress(raw))

			res, err := resp.BodyUncompressed()
			require.NoError(t, err)
			assert.Equal(t, raw, res)
		})
	}

	t.Run("unsupported_encoding", func(t *testing.T) {
		var resp h1engine.Response
		resp.Header.Set("Content-Encoding", "unknown-alg-xyz")
		resp.SetBody([]byte("some data"))

		_, err := resp.BodyUncompressed()
		require.Error(t, err)
		assert.ErrorIs(t, err, h1engine.ErrContentEncodingUnsupported)
	})
}

func TestH1Engine_Request_BodyUncompressed(t *testing.T) {
	t.Parallel()

	raw := []byte("Request uncompressed payload testing")

	var req h1engine.Request
	req.Header.Set("Content-Encoding", "gzip")
	req.SetBody(h1engine.AppendGzipBytes(nil, raw))

	res, err := req.BodyUncompressed()
	require.NoError(t, err)
	assert.Equal(t, raw, res)
}
