// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flate_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/compress/flate"
)

func TestFlateWriterAndReader(t *testing.T) {
	t.Parallel()

	data := []byte(strings.Repeat("Flate RFC 1951 test payload with repeated sequences. ", 20))

	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	require.NoError(t, err)

	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	r := flate.NewReader(&buf)
	decompressed, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, data, decompressed)
	require.NoError(t, r.Close())
}

func TestFlateStateless(t *testing.T) {
	t.Parallel()

	data := []byte("Stateless flate compression test.")
	var buf bytes.Buffer

	err := flate.StatelessDeflate(&buf, data, true, nil)
	require.NoError(t, err)

	r := flate.NewReader(&buf)
	decompressed, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, data, decompressed)
	require.NoError(t, r.Close())
}

func TestMatchLen_Comprehensive(t *testing.T) {
	t.Parallel()

	s1 := []byte("The quick brown fox jumps over the lazy dog 1234567890abcdefghijklmnopqrstuvwxyz")
	s2 := []byte("The quick brown fox jumps over the lazy dog 1234567890abcdefghijklmnopqrstuvwxyz")

	// Full match
	assert.Equal(t, len(s1), flate.ExportMatchLen(s1, s2))

	// Prefix mismatch at various offsets
	for i := 0; i < len(s1); i++ {
		s2Copy := append([]byte(nil), s2...)
		s2Copy[i] ^= 0xFF
		assert.Equal(t, i, flate.ExportMatchLen(s1, s2Copy))
	}
}

func BenchmarkMatchLen_AVX2(b *testing.B) {
	s1 := []byte(strings.Repeat("abcdefghijklmnopqrstuvwxyz123456", 10))
	s2 := []byte(strings.Repeat("abcdefghijklmnopqrstuvwxyz123456", 10))
	s2[250] = 'Z' // Mismatch at byte 250

	b.SetBytes(250)
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = flate.ExportMatchLen(s1, s2)
	}
}
