// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"fmt"
	"io"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
	"github.com/lemon4ksan/aoni/internal/quic/quicvarint"
)

func TestParseMaxStreamsFrameBidirectional(t *testing.T) {
	data := encodeVarInt(0xdecaf)
	f, l, err := parseMaxStreamsFrame(data, FrameTypeBidiMaxStreams, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, protocol.StreamTypeBidi, f.Type)
	require.EqualValues(t, 0xdecaf, f.MaxStreamNum)
	require.Equal(t, len(data), l)
}

func TestParseMaxStreamsFrameUnidirectional(t *testing.T) {
	data := encodeVarInt(0xdecaf)
	f, l, err := parseMaxStreamsFrame(data, FrameTypeUniMaxStreams, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, protocol.StreamTypeUni, f.Type)
	require.EqualValues(t, 0xdecaf, f.MaxStreamNum)
	require.Equal(t, len(data), l)
}

func TestParseMaxStreamsErrorsOnEOF(t *testing.T) {
	const typ = 0x1d

	data := encodeVarInt(0xdeadbeefcafe13)
	_, l, err := parseMaxStreamsFrame(data, typ, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, len(data), l)

	for i := range data {
		_, _, err := parseMaxStreamsFrame(data[:i], typ, protocol.Version1)
		require.Equal(t, io.EOF, err)
	}
}

func TestParseMaxStreamsMaxValue(t *testing.T) {
	for _, streamType := range []protocol.StreamType{protocol.StreamTypeUni, protocol.StreamTypeBidi} {
		var streamTypeStr string
		if streamType == protocol.StreamTypeUni {
			streamTypeStr = "unidirectional"
		} else {
			streamTypeStr = "bidirectional"
		}

		t.Run(streamTypeStr, func(t *testing.T) {
			f := &MaxStreamsFrame{
				Type:         streamType,
				MaxStreamNum: protocol.MaxStreamCount,
			}
			b, err := f.Append(nil, protocol.Version1)
			require.NoError(t, err)
			typ, l, err := quicvarint.Parse(b)
			require.NoError(t, err)

			b = b[l:]
			frame, _, err := parseMaxStreamsFrame(b, FrameType(typ), protocol.Version1)
			require.NoError(t, err)
			require.Equal(t, f, frame)
		})
	}
}

func TestParseMaxStreamsErrorsOnTooLargeStreamCount(t *testing.T) {
	for _, streamType := range []protocol.StreamType{protocol.StreamTypeUni, protocol.StreamTypeBidi} {
		var streamTypeStr string
		if streamType == protocol.StreamTypeUni {
			streamTypeStr = "unidirectional"
		} else {
			streamTypeStr = "bidirectional"
		}

		t.Run(streamTypeStr, func(t *testing.T) {
			f := &MaxStreamsFrame{
				Type:         streamType,
				MaxStreamNum: protocol.MaxStreamCount + 1,
			}
			b, err := f.Append(nil, protocol.Version1)
			require.NoError(t, err)
			typ, l, err := quicvarint.Parse(b)
			require.NoError(t, err)

			b = b[l:]
			_, _, err = parseMaxStreamsFrame(b, FrameType(typ), protocol.Version1)
			require.EqualError(t, err, fmt.Sprintf("%d exceeds the maximum stream count", protocol.MaxStreamCount+1))
		})
	}
}

func TestWriteMaxStreamsBidirectional(t *testing.T) {
	f := &MaxStreamsFrame{
		Type:         protocol.StreamTypeBidi,
		MaxStreamNum: 0xdeadbeef,
	}
	b, err := f.Append(nil, protocol.Version1)
	require.NoError(t, err)

	expected := []byte{byte(FrameTypeBidiMaxStreams)}
	expected = append(expected, encodeVarInt(0xdeadbeef)...)
	require.Equal(t, expected, b)
	require.Len(t, b, int(f.Length(protocol.Version1)))
}

func TestWriteMaxStreamsUnidirectional(t *testing.T) {
	f := &MaxStreamsFrame{
		Type:         protocol.StreamTypeUni,
		MaxStreamNum: 0xdecafbad,
	}
	b, err := f.Append(nil, protocol.Version1)
	require.NoError(t, err)

	expected := []byte{byte(FrameTypeUniMaxStreams)}
	expected = append(expected, encodeVarInt(0xdecafbad)...)
	require.Equal(t, expected, b)
	require.Len(t, b, int(f.Length(protocol.Version1)))
}
