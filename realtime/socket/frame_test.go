// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package socket_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/realtime/socket"
)

func TestFrameBuffer_PoolAndMethods(t *testing.T) {
	t.Parallel()

	fb := socket.AcquireFrameBuffer(11)
	require.NotNil(t, fb)
	assert.Equal(t, 11, fb.Len())
	assert.Equal(t, 11, len(fb.Bytes()))

	copy(fb.Bytes(), []byte("hello world"))
	assert.True(t, fb.Equal(fb.Bytes()))

	fb2 := socket.AcquireFrameBuffer(11)
	copy(fb2.Bytes(), []byte("hello world"))
	assert.True(t, fb.Equal(fb2))

	fb.Reset()
	assert.Equal(t, 0, fb.Len())

	socket.ReleaseFrameBuffer(fb)
	socket.ReleaseFrameBuffer(fb2)
	socket.ReleaseFrameBuffer(nil)
}

func TestLengthPrefixedFramer_RoundTrip(t *testing.T) {
	t.Parallel()

	framer := socket.NewLengthPrefixedFramer(socket.LengthPrefixedConfig{
		ByteOrder: binary.LittleEndian,
		Magic:     []byte("VT01"),
		MaxLength: 1024 * 1024,
	})

	payload := []byte("high performance framing test data")

	var buf bytes.Buffer

	err := framer.WriteFrame(&buf, payload)
	require.NoError(t, err)

	fb, err := framer.ReadFrame(&buf)
	require.NoError(t, err)

	defer socket.ReleaseFrameBuffer(fb)

	assert.Equal(t, payload, fb.Bytes())
}

func TestLengthPrefixedFramer_InvalidMagic(t *testing.T) {
	t.Parallel()

	framer := socket.NewLengthPrefixedFramer(socket.LengthPrefixedConfig{
		ByteOrder: binary.LittleEndian,
		Magic:     []byte("VT01"),
	})

	var buf bytes.Buffer

	_ = binary.Write(&buf, binary.LittleEndian, uint32(5))
	buf.WriteString("XXXX")
	buf.WriteString("12345")

	_, err := framer.ReadFrame(&buf)
	require.ErrorIs(t, err, socket.ErrInvalidMagic)
}

func TestLengthPrefixedFramer_FrameTooLarge(t *testing.T) {
	t.Parallel()

	framer := socket.NewLengthPrefixedFramer(socket.LengthPrefixedConfig{
		ByteOrder: binary.LittleEndian,
		MaxLength: 10,
	})

	var buf bytes.Buffer

	_ = binary.Write(&buf, binary.LittleEndian, uint32(100))
	buf.Write(make([]byte, 100))

	_, err := framer.ReadFrame(&buf)
	require.ErrorIs(t, err, socket.ErrFrameTooLarge)
}

func TestLengthPrefixedFramer_EOF(t *testing.T) {
	t.Parallel()

	framer := socket.NewLengthPrefixedFramer(socket.LengthPrefixedConfig{})

	var buf bytes.Buffer

	_, err := framer.ReadFrame(&buf)
	require.ErrorIs(t, err, io.EOF)
}
