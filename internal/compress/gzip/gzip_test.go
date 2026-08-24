// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gzip_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/compress/gzip"
)

func TestGzipWriterAndReader(t *testing.T) {
	t.Parallel()

	data := []byte("Testing standalone gzip reader and writer compression in aoni.")

	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Name = "test.txt"
	w.Comment = "unit test"

	n, err := w.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)

	err = w.Close()
	require.NoError(t, err)

	r, err := gzip.NewReader(&buf)
	require.NoError(t, err)
	assert.Equal(t, "test.txt", r.Name)
	assert.Equal(t, "unit test", r.Comment)

	decompressed, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, data, decompressed)

	err = r.Close()
	require.NoError(t, err)
}

func TestGzipWriterLevels(t *testing.T) {
	t.Parallel()

	data := []byte(strings.Repeat("Gzip compression levels test content. ", 30))

	levels := []int{
		gzip.BestSpeed,
		gzip.BestCompression,
		gzip.DefaultCompression,
		gzip.HuffmanOnly,
		gzip.StatelessCompression,
	}

	for _, level := range levels {
		var buf bytes.Buffer
		w, err := gzip.NewWriterLevel(&buf, level)
		require.NoError(t, err)

		_, err = w.Write(data)
		require.NoError(t, err)
		require.NoError(t, w.Close())

		r, err := gzip.NewReader(&buf)
		require.NoError(t, err)

		decompressed, err := io.ReadAll(r)
		require.NoError(t, err)
		assert.Equal(t, data, decompressed)
	}

	// Invalid level
	_, err := gzip.NewWriterLevel(&bytes.Buffer{}, 99)
	assert.Error(t, err)
}
