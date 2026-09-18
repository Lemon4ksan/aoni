// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package decode_test

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/lemon4ksan/foundation/testing/assert"
	"github.com/lemon4ksan/foundation/testing/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/lemon4ksan/aoni/x/codec/decode"
)

func buildTestFrame(flags byte, payload []byte) []byte {
	buf := make([]byte, 5+len(payload))
	buf[0] = flags
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(payload)))
	copy(buf[5:], payload)

	return buf
}

func TestRepro_GRPCWebDecoder_TruncatedSecondFramePayload(t *testing.T) {
	t.Parallel()

	pbData, err := proto.Marshal(wrapperspb.String("first_valid_message"))
	require.NoError(t, err)

	frame1 := buildTestFrame(0x00, pbData)

	// Incomplete frame 2: declares 50 bytes, only provides 4 bytes
	var frame2Header [5]byte

	frame2Header[0] = 0x00
	binary.BigEndian.PutUint32(frame2Header[1:5], 50)
	frame2Partial := slices.Concat(frame2Header[:], []byte("abcd"))

	stream := slices.Concat(frame1, frame2Partial)

	var target wrapperspb.StringValue

	decodeErr := decode.GRPCWebDecoder.Decode(bytes.NewReader(stream), &target)

	require.Error(t, decodeErr, "truncated second frame payload must return an error, but returned nil")

	var grpcErr *decode.GRPCWebError
	require.ErrorAs(t, decodeErr, &grpcErr)
	assert.Equal(t, "read_payload", grpcErr.Op)
}

func TestRepro_GRPCWebDecoder_TruncatedTrailerFramePayload(t *testing.T) {
	t.Parallel()

	pbData, err := proto.Marshal(wrapperspb.String("first_valid_message"))
	require.NoError(t, err)

	frame1 := buildTestFrame(0x00, pbData)

	// Incomplete trailer frame (0x80): declares 60 bytes, only provides 5 bytes
	var trailerHeader [5]byte

	trailerHeader[0] = 0x80
	binary.BigEndian.PutUint32(trailerHeader[1:5], 60)
	trailerPartial := slices.Concat(trailerHeader[:], []byte("grpc-"))

	stream := slices.Concat(frame1, trailerPartial)

	var target wrapperspb.StringValue

	decodeErr := decode.GRPCWebDecoder.Decode(bytes.NewReader(stream), &target)

	require.Error(t, decodeErr, "truncated trailer frame payload must return an error, but returned nil")

	var grpcErr *decode.GRPCWebError
	require.ErrorAs(t, decodeErr, &grpcErr)
	assert.Equal(t, "read_payload", grpcErr.Op)
}

func TestRepro_GRPCWebDecoder_TruncatedSecondFrameHeader(t *testing.T) {
	t.Parallel()

	pbData, err := proto.Marshal(wrapperspb.String("first_valid_message"))
	require.NoError(t, err)

	frame1 := buildTestFrame(0x00, pbData)
	// Trailing 2 bytes (incomplete header)
	stream := slices.Concat(frame1, []byte{0x00, 0x01})

	var target wrapperspb.StringValue

	decodeErr := decode.GRPCWebDecoder.Decode(bytes.NewReader(stream), &target)

	require.Error(t, decodeErr, "trailing incomplete header must return an error, but returned nil")

	var grpcErr *decode.GRPCWebError
	require.ErrorAs(t, decodeErr, &grpcErr)
	assert.Equal(t, "read_header", grpcErr.Op)
}

type uninspectableReader struct {
	r *bytes.Reader
}

func (u uninspectableReader) Read(p []byte) (int, error) {
	return u.r.Read(p)
}

func TestRepro_GRPCWebDecoder_StreamTruncatedSecondFramePayload(t *testing.T) {
	t.Parallel()

	pbData, err := proto.Marshal(wrapperspb.String("first_valid_message"))
	require.NoError(t, err)

	frame1 := buildTestFrame(0x00, pbData)

	var frame2Header [5]byte

	frame2Header[0] = 0x00
	binary.BigEndian.PutUint32(frame2Header[1:5], 50)
	frame2Partial := slices.Concat(frame2Header[:], []byte("abcd"))

	stream := slices.Concat(frame1, frame2Partial)

	var target wrapperspb.StringValue

	decodeErr := decode.GRPCWebDecoder.Decode(uninspectableReader{r: bytes.NewReader(stream)}, &target)

	require.Error(t, decodeErr, "streaming truncated second frame payload must return an error")

	var grpcErr *decode.GRPCWebError
	require.ErrorAs(t, decodeErr, &grpcErr)
	assert.Equal(t, "read_payload", grpcErr.Op)
}

func TestRepro_GRPCWebDecoder_StreamTruncatedTrailerFramePayload(t *testing.T) {
	t.Parallel()

	pbData, err := proto.Marshal(wrapperspb.String("first_valid_message"))
	require.NoError(t, err)

	frame1 := buildTestFrame(0x00, pbData)

	var trailerHeader [5]byte

	trailerHeader[0] = 0x80
	binary.BigEndian.PutUint32(trailerHeader[1:5], 60)
	trailerPartial := slices.Concat(trailerHeader[:], []byte("grpc-"))

	stream := slices.Concat(frame1, trailerPartial)

	var target wrapperspb.StringValue

	decodeErr := decode.GRPCWebDecoder.Decode(uninspectableReader{r: bytes.NewReader(stream)}, &target)

	require.Error(t, decodeErr, "streaming truncated trailer frame payload must return an error")

	var grpcErr *decode.GRPCWebError
	require.ErrorAs(t, decodeErr, &grpcErr)
	assert.Equal(t, "read_payload", grpcErr.Op)
}
