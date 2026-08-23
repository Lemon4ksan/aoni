// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
	"github.com/lemon4ksan/aoni/internal/quic/internal/qerr"
)

func TestParseStopSending(t *testing.T) {
	data := encodeVarInt(0xdecafbad)             // stream ID
	data = append(data, encodeVarInt(0x1337)...) // error code
	frame, l, err := parseStopSendingFrame(data, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, protocol.StreamID(0xdecafbad), frame.StreamID)
	require.Equal(t, qerr.StreamErrorCode(0x1337), frame.ErrorCode)
	require.Equal(t, len(data), l)
}

func TestParseStopSendingErrorsOnEOFs(t *testing.T) {
	data := encodeVarInt(0xdecafbad)               // stream ID
	data = append(data, encodeVarInt(0x123456)...) // error code
	_, l, err := parseStopSendingFrame(data, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, len(data), l)

	for i := range data {
		_, _, err := parseStopSendingFrame(data[:i], protocol.Version1)
		require.Equal(t, io.EOF, err)
	}
}

func TestWriteStopSendingFrame(t *testing.T) {
	frame := &StopSendingFrame{
		StreamID:  0xdeadbeefcafe,
		ErrorCode: 0xdecafbad,
	}
	b, err := frame.Append(nil, protocol.Version1)
	require.NoError(t, err)

	expected := []byte{byte(FrameTypeStopSending)}
	expected = append(expected, encodeVarInt(0xdeadbeefcafe)...)
	expected = append(expected, encodeVarInt(0xdecafbad)...)
	require.Equal(t, expected, b)
	require.Len(t, b, int(frame.Length(protocol.Version1)))
}
