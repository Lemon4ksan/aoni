// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package qpack

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestQPACK_EncoderDecoderRoundtrip(t *testing.T) {
	t.Parallel()

	testFields := []HeaderField{
		{Name: ":method", Value: "GET"},
		{Name: ":scheme", Value: "https"},
		{Name: ":authority", Value: "example.com"},
		{Name: ":path", Value: "/index.html"},
		{Name: "user-agent", Value: "aoni-qpack/1.0"},
		{Name: "custom-header", Value: "custom-value-12345"},
		{Name: ":status", Value: "200"},
		{Name: "content-type", Value: "application/json"},
	}

	var buf bytes.Buffer

	enc := NewEncoder(&buf)

	for _, hf := range testFields {
		err := enc.WriteField(hf)
		require.NoError(t, err)
	}

	dec := NewDecoder()
	decodeFn := dec.Decode(buf.Bytes())

	var decoded []HeaderField

	for {
		hf, err := decodeFn()
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)

		decoded = append(decoded, hf)
	}

	require.Len(t, decoded, len(testFields))

	for i, expected := range testFields {
		assert.Equal(t, expected.Name, decoded[i].Name)
		assert.Equal(t, expected.Value, decoded[i].Value)
	}
}

func TestQPACK_RFC9204AppendixBExamples(t *testing.T) {
	t.Parallel()

	// Example B.1: Literal Field Line with Name Reference (Static Table)
	// :authority: www.example.com
	var buf bytes.Buffer

	enc := NewEncoder(&buf)
	err := enc.WriteField(HeaderField{Name: ":authority", Value: "www.example.com"})
	require.NoError(t, err)

	dec := NewDecoder()
	decodeFn := dec.Decode(buf.Bytes())

	hf, err := decodeFn()
	require.NoError(t, err)
	assert.Equal(t, ":authority", hf.Name)
	assert.Equal(t, "www.example.com", hf.Value)
	assert.True(t, hf.IsPseudo())

	_, err = decodeFn()
	assert.ErrorIs(t, err, io.EOF)
}

func TestQPACK_Varint(t *testing.T) {
	t.Parallel()

	testVals := []uint64{0, 1, 15, 16, 31, 32, 63, 64, 127, 128, 255, 1337, 65535, 1 << 20}
	for _, val := range testVals {
		for prefixLen := uint8(2); prefixLen <= 8; prefixLen++ {
			var dst []byte

			dst = append(dst, 0)
			dst = appendInt(dst, prefixLen, val)

			readVal, n, err := readInt(prefixLen, dst)
			require.NoError(t, err)
			assert.Equal(t, val, readVal)
			assert.Equal(t, len(dst), n)
		}
	}
}
