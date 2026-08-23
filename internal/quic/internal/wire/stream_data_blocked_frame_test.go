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

func TestParseStreamDataBlocked(t *testing.T) {
	data := encodeVarInt(0xdeadbeef)                 // stream ID
	data = append(data, encodeVarInt(0xdecafbad)...) // offset
	frame, l, err := parseStreamDataBlockedFrame(data, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, protocol.StreamID(0xdeadbeef), frame.StreamID)
	require.Equal(t, protocol.ByteCount(0xdecafbad), frame.MaximumStreamData)
	require.Equal(t, len(data), l)
}

func TestParseStreamDataBlockedErrorsOnEOFs(t *testing.T) {
	data := encodeVarInt(0xdeadbeef)
	data = append(data, encodeVarInt(0xc0010ff)...)
	_, l, err := parseStreamDataBlockedFrame(data, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, len(data), l)

	for i := range data {
		_, _, err := parseStreamDataBlockedFrame(data[:i], protocol.Version1)
		require.Equal(t, io.EOF, err)
	}
}

func TestWriteStreamDataBlocked(t *testing.T) {
	f := &StreamDataBlockedFrame{
		StreamID:          0xdecafbad,
		MaximumStreamData: 0x1337,
	}
	b, err := f.Append(nil, protocol.Version1)
	require.NoError(t, err)

	expected := []byte{byte(FrameTypeStreamDataBlocked)}
	expected = append(expected, encodeVarInt(uint64(f.StreamID))...)
	expected = append(expected, encodeVarInt(uint64(f.MaximumStreamData))...)
	require.Equal(t, expected, b)
	require.Equal(t, int(f.Length(protocol.Version1)), len(b))
}
