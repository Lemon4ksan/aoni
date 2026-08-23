// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
)

func TestParseRetireConnectionID(t *testing.T) {
	data := encodeVarInt(0xdeadbeef) // sequence number
	frame, l, err := parseRetireConnectionIDFrame(data, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, uint64(0xdeadbeef), frame.SequenceNumber)
	require.Equal(t, len(data), l)
}

func TestParseRetireConnectionIDErrorsOnEOFs(t *testing.T) {
	data := encodeVarInt(0xdeadbeef) // sequence number
	_, l, err := parseRetireConnectionIDFrame(data, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, len(data), l)

	for i := range data {
		_, _, err := parseRetireConnectionIDFrame(data[:i], protocol.Version1)
		require.Equal(t, io.EOF, err)
	}
}

func TestWriteRetireConnectionID(t *testing.T) {
	frame := &RetireConnectionIDFrame{SequenceNumber: 0x1337}
	b, err := frame.Append(nil, protocol.Version1)
	require.NoError(t, err)

	expected := []byte{byte(FrameTypeRetireConnectionID)}
	expected = append(expected, encodeVarInt(0x1337)...)
	require.Equal(t, expected, b)
	require.Len(t, b, int(frame.Length(protocol.Version1)))
}
