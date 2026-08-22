// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flate_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
