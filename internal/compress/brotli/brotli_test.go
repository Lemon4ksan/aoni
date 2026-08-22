// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package brotli_test

import (
	"bytes"
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
