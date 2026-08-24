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

func TestParsePathResponse(t *testing.T) {
	b := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	f, l, err := parsePathResponseFrame(b, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, [8]byte{1, 2, 3, 4, 5, 6, 7, 8}, f.Data)
	require.Equal(t, len(b), l)
}

func TestParsePathResponseErrorsOnEOFs(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	_, l, err := parsePathResponseFrame(data, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, len(data), l)

	for i := range data {
		_, _, err := parsePathResponseFrame(data[:i], protocol.Version1)
		require.Equal(t, io.EOF, err)
	}
}

func TestWritePathResponse(t *testing.T) {
	frame := PathResponseFrame{Data: [8]byte{0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0x13, 0x37}}
	b, err := frame.Append(nil, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, []byte{byte(FrameTypePathResponse), 0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0x13, 0x37}, b)
	require.Len(t, b, int(frame.Length(protocol.Version1)))
}
