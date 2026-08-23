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

func TestParseNewTokenFrame(t *testing.T) {
	token := "Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."
	data := encodeVarInt(uint64(len(token)))
	data = append(data, token...)
	f, l, err := parseNewTokenFrame(data, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, token, string(f.Token))
	require.Equal(t, len(data), l)
}

func TestParseNewTokenFrameRejectsEmptyTokens(t *testing.T) {
	data := encodeVarInt(0)
	_, _, err := parseNewTokenFrame(data, protocol.Version1)
	require.EqualError(t, err, "token must not be empty")
}

func TestParseNewTokenFrameErrorsOnEOFs(t *testing.T) {
	token := "Lorem ipsum dolor sit amet, consectetur adipiscing elit"
	data := encodeVarInt(uint64(len(token)))
	data = append(data, token...)
	_, l, err := parseNewTokenFrame(data, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, len(data), l)

	for i := range data {
		_, _, err := parseNewTokenFrame(data[:i], protocol.Version1)
		require.Equal(t, io.EOF, err)
	}
}

func TestWriteNewTokenFrame(t *testing.T) {
	token := "Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat."
	f := &NewTokenFrame{Token: []byte(token)}
	b, err := f.Append(nil, protocol.Version1)
	require.NoError(t, err)

	expected := []byte{byte(FrameTypeNewToken)}
	expected = append(expected, encodeVarInt(uint64(len(token)))...)
	expected = append(expected, token...)
	require.Equal(t, expected, b)
	require.Equal(t, len(b), int(f.Length(protocol.Version1)))
}
