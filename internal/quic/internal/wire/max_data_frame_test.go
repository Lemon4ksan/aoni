// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"io"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
)

func TestParseMaxDataFrame(t *testing.T) {
	data := encodeVarInt(0xdecafbad123456) // byte offset
	frame, l, err := parseMaxDataFrame(data, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, protocol.ByteCount(0xdecafbad123456), frame.MaximumData)
	require.Equal(t, len(data), l)
}

func TestParseMaxDataErrorsOnEOFs(t *testing.T) {
	data := encodeVarInt(0xdecafbad1234567) // byte offset
	_, l, err := parseMaxDataFrame(data, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, len(data), l)

	for i := range data {
		_, _, err := parseMaxDataFrame(data[:i], protocol.Version1)
		require.Equal(t, io.EOF, err)
	}
}

func TestWriteMaxDataFrame(t *testing.T) {
	f := &MaxDataFrame{MaximumData: 0xdeadbeefcafe}
	b, err := f.Append(nil, protocol.Version1)
	require.NoError(t, err)

	expected := []byte{byte(FrameTypeMaxData)}
	expected = append(expected, encodeVarInt(0xdeadbeefcafe)...)
	require.Equal(t, expected, b)
	require.Len(t, b, int(f.Length(protocol.Version1)))
}
