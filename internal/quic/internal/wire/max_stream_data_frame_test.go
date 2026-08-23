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

func TestParseMaxStreamFrame(t *testing.T) {
	data := encodeVarInt(0xdeadbeef)                 // Stream ID
	data = append(data, encodeVarInt(0x12345678)...) // Offset
	frame, l, err := parseMaxStreamDataFrame(data, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, protocol.StreamID(0xdeadbeef), frame.StreamID)
	require.Equal(t, protocol.ByteCount(0x12345678), frame.MaximumStreamData)
	require.Equal(t, len(data), l)
}

func TestParseMaxStreamDataErrorsOnEOFs(t *testing.T) {
	data := encodeVarInt(0xdeadbeef)                 // Stream ID
	data = append(data, encodeVarInt(0x12345678)...) // Offset
	_, l, err := parseMaxStreamDataFrame(data, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, len(data), l)

	for i := range data {
		_, _, err := parseMaxStreamDataFrame(data[:i], protocol.Version1)
		require.Equal(t, io.EOF, err)
	}
}

func TestWriteMaxStreamDataFrame(t *testing.T) {
	f := &MaxStreamDataFrame{
		StreamID:          0xdecafbad,
		MaximumStreamData: 0xdeadbeefcafe42,
	}
	expected := []byte{byte(FrameTypeMaxStreamData)}
	expected = append(expected, encodeVarInt(0xdecafbad)...)
	expected = append(expected, encodeVarInt(0xdeadbeefcafe42)...)
	b, err := f.Append(nil, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, expected, b)
	require.Equal(t, len(b), int(f.Length(protocol.Version1)))
}
