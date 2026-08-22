// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni/internal/compress/brotli"
)

func TestBrotliDecompression(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		data string
	}{
		{
			name: "short_string",
			data: "Hello, Brotli RFC 7932 decompression engine!",
		},
		{
			name: "repeated_text",
			data: strings.Repeat("aoni-brotli-zero-allocation-engine ", 100),
		},
		{
			name: "json_payload",
			data: `{"id": 42, "status": "ok", "message": "brotli decompressed", "tags": ["fast", "zero-alloc", "aoni"]}`,
		},
		{
			name: "empty_string",
			data: "",
		},
		{
			name: "large_html_payload",
			data: strings.Repeat(
				"<!DOCTYPE html><html><head><title>Test Page</title></head><body><h1>Hello World</h1><p>Lorem ipsum dolor sit amet, consectetur adipiscing elit.</p></body></html>\n",
				300,
			),
		},
		{
			name: "binary_sequence",
			data: string(bytes.Repeat([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 255, 254, 253, 128, 64}, 1000)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			compressed := fasthttp.AppendBrotliBytes(nil, []byte(tc.data))

			r := brotli.NewReader(bytes.NewReader(compressed))
			decompressed, err := io.ReadAll(r)
			require.NoError(t, err)
			assert.Equal(t, tc.data, string(decompressed))

			// Test Reset
			err = r.Reset(bytes.NewReader(compressed))
			require.NoError(t, err)
			decompressed2, err := io.ReadAll(r)
			require.NoError(t, err)
			assert.Equal(t, tc.data, string(decompressed2))
		})
	}
}

func TestBrotliInvalidData(t *testing.T) {
	t.Parallel()

	invalidPayload := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02}
	r := brotli.NewReader(bytes.NewReader(invalidPayload))
	_, err := io.ReadAll(r)
	assert.Error(t, err)
}

func TestBrotliChunkedReading(t *testing.T) {
	t.Parallel()

	data := strings.Repeat("Testing chunked streaming decompression with various small buffer reads.", 50)
	compressed := fasthttp.AppendBrotliBytes(nil, []byte(data))

	r := brotli.NewReader(bytes.NewReader(compressed))
	buf := make([]byte, 17) // prime chunk size

	var out bytes.Buffer

	for {
		n, err := r.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			require.NoError(t, err)
		}
	}

	assert.Equal(t, data, out.String())
}

func TestBrotliDecompressHelper(t *testing.T) {
	t.Parallel()

	raw := []byte("Standalone Brotli Decompress helper test with slice destination.")
	compressed := fasthttp.AppendBrotliBytes(nil, raw)

	// Test nil dst
	decompressed, err := brotli.Decompress(nil, compressed)
	require.NoError(t, err)
	assert.Equal(t, raw, decompressed)

	// Test preallocated dst
	dst := make([]byte, 0, len(raw))
	decompressed2, err := brotli.Decompress(dst, compressed)
	require.NoError(t, err)
	assert.Equal(t, raw, decompressed2)

	// Test empty src
	emptyDecompressed, err := brotli.Decompress(nil, nil)
	require.NoError(t, err)
	assert.Empty(t, emptyDecompressed)
}

func TestBrotliReaderPoolAndCloser(t *testing.T) {
	t.Parallel()

	raw := []byte("Testing AcquireReader, ReleaseReader, and Close (io.ReadCloser).")
	compressed := fasthttp.AppendBrotliBytes(nil, raw)

	r := brotli.AcquireReader(bytes.NewReader(compressed))

	var rc io.ReadCloser = r

	decompressed, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, raw, decompressed)

	err = rc.Close()
	require.NoError(t, err)

	brotli.ReleaseReader(r)
}

func BenchmarkBrotliDecompress(b *testing.B) {
	data := []byte(strings.Repeat(
		"<!DOCTYPE html><html><head><title>Benchmark Page</title></head><body>"+
			"<h1>High Performance Brotli RFC 7932 Engine</h1><p>Zero allocation streaming decoder in Go.</p>"+
			"</body></html>\n",
		100,
	))
	compressed := fasthttp.AppendBrotliBytes(nil, data)

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r := brotli.NewReader(bytes.NewReader(compressed))

		_, err := io.Copy(io.Discard, r)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBrotliDecompressReuse(b *testing.B) {
	data := []byte(strings.Repeat(
		"<!DOCTYPE html><html><head><title>Benchmark Page</title></head><body>"+
			"<h1>High Performance Brotli RFC 7932 Engine</h1><p>Zero allocation streaming decoder in Go.</p>"+
			"</body></html>\n",
		100,
	))
	compressed := fasthttp.AppendBrotliBytes(nil, data)

	r := brotli.NewReader(bytes.NewReader(compressed))
	src := bytes.NewReader(compressed)
	copyBuf := make([]byte, 32*1024)

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		src.Reset(compressed)

		if err := r.Reset(src); err != nil {
			b.Fatal(err)
		}

		_, err := io.CopyBuffer(io.Discard, r, copyBuf)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompare_SmallPayload_1KB(b *testing.B) {
	data := []byte(`{"id": 42, "user": "aoni-architect", "status": "active", "meta": {"session": "xyz-123"}}`)
	compressed := fasthttp.AppendBrotliBytes(nil, data)
	dst := make([]byte, 0, len(data))

	b.Run("fasthttp_unbrotli", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			var err error

			dst, err = fasthttp.AppendUnbrotliBytes(dst[:0], compressed)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("aoni_brotli_decompress", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			var err error

			dst, err = brotli.Decompress(dst[:0], compressed)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkCompare_MediumPayload_18KB(b *testing.B) {
	data := []byte(strings.Repeat(
		"<!DOCTYPE html><html><head><title>Benchmark Page</title></head><body>"+
			"<h1>High Performance Brotli RFC 7932 Engine</h1><p>Zero allocation streaming decoder in Go.</p>"+
			"</body></html>\n",
		100,
	))
	compressed := fasthttp.AppendBrotliBytes(nil, data)
	dst := make([]byte, 0, len(data))

	b.Run("fasthttp_unbrotli", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			var err error

			dst, err = fasthttp.AppendUnbrotliBytes(dst[:0], compressed)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("aoni_brotli_decompress", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			var err error

			dst, err = brotli.Decompress(dst[:0], compressed)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkCompare_LargePayload_100KB(b *testing.B) {
	data := []byte(strings.Repeat(
		"<!DOCTYPE html><html><head><title>Large Benchmark Document</title></head><body>"+
			"<article><section><p>Ultra high performance internet protocol engine for Go.</p></section></article>"+
			"</body></html>\n",
		450,
	))
	compressed := fasthttp.AppendBrotliBytes(nil, data)
	dst := make([]byte, 0, len(data))

	b.Run("fasthttp_unbrotli", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			var err error

			dst, err = fasthttp.AppendUnbrotliBytes(dst[:0], compressed)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("aoni_brotli_decompress", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(data)))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			var err error

			dst, err = brotli.Decompress(dst[:0], compressed)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
