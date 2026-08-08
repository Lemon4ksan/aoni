// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc_test

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/lemon4ksan/aoni/grpc"
)

func TestMarshalAndUnmarshalFrame(t *testing.T) {
	t.Parallel()

	t.Run("uncompressed_frame", func(t *testing.T) {
		t.Parallel()

		msg := wrapperspb.String("hello grpc")
		frame, err := grpc.MarshalFrame(msg, false)
		require.NoError(t, err)

		assert.Equal(t, byte(0x00), frame[0])
		assert.Greater(t, len(frame), 5)

		var target wrapperspb.StringValue

		compressed, err := grpc.UnmarshalFrame(bytes.NewReader(frame), &target)
		require.NoError(t, err)
		assert.False(t, compressed)
		assert.Equal(t, "hello grpc", target.GetValue())
	})

	t.Run("compressed_frame", func(t *testing.T) {
		t.Parallel()

		msg := wrapperspb.String("compressed grpc message payload")
		frame, err := grpc.MarshalFrameCompressed(msg, true)
		require.NoError(t, err)

		assert.Equal(t, byte(0x01), frame[0])

		var target wrapperspb.StringValue

		compressed, err := grpc.UnmarshalFrame(bytes.NewReader(frame), &target)
		require.NoError(t, err)
		assert.True(t, compressed)
		assert.Equal(t, "compressed grpc message payload", target.GetValue())
	})

	t.Run("nil_message_marshal", func(t *testing.T) {
		t.Parallel()

		frame, err := grpc.MarshalFrame(nil, false)
		require.NoError(t, err)
		assert.Equal(t, []byte{0, 0, 0, 0, 0}, frame)
	})
}

func TestBinaryHeaderEncoding(t *testing.T) {
	t.Parallel()

	rawBytes := []byte{0x01, 0x02, 0x03, 0xFF, 0xFE}
	encoded := grpc.EncodeBinaryHeader(rawBytes)

	decoded, err := grpc.DecodeBinaryHeader(encoded)
	require.NoError(t, err)
	assert.Equal(t, rawBytes, decoded)

	// Test standard padding base64 decode fallback
	paddedEncoded := base64.StdEncoding.EncodeToString(rawBytes)
	decodedPadded, err := grpc.DecodeBinaryHeader(paddedEncoded)
	require.NoError(t, err)
	assert.Equal(t, rawBytes, decodedPadded)
}

func TestFormatTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		duration time.Duration
		expected string
	}{
		{500 * time.Millisecond, "500m"},
		{2 * time.Second, "2S"},
		{5 * time.Minute, "5M"},
		{1 * time.Hour, "1H"},
	}

	for _, tt := range tests {
		formatted := grpc.FormatTimeout(tt.duration)
		assert.Equal(t, tt.expected, formatted)
	}
}

func TestStatusCodeStrings(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "OK", grpc.StatusOK.String())
	assert.Equal(t, "CANCELLED", grpc.StatusCancelled.String())
	assert.Equal(t, "UNAUTHENTICATED", grpc.StatusUnauthenticated.String())
	assert.Equal(t, "UNKNOWN", grpc.StatusUnknown.String())
	assert.Equal(t, "CODE_99", grpc.StatusCode(99).String())
}

func TestStatusErrorFormatting(t *testing.T) {
	t.Parallel()

	errSimple := &grpc.StatusError{
		Code:    grpc.StatusPermissionDenied,
		Message: "access denied",
	}
	assert.Contains(t, errSimple.Error(), "PERMISSION_DENIED")
	assert.Contains(t, errSimple.Error(), "access denied")

	errWithDetails := &grpc.StatusError{
		Code:       grpc.StatusInvalidArgument,
		Message:    "invalid field",
		RawDetails: []byte{0x01, 0x02, 0x03},
		Trailer:    http.Header{"grpc-status": []string{"3"}},
	}
	assert.Contains(t, errWithDetails.Error(), "INVALID_ARGUMENT")
	assert.Contains(t, errWithDetails.Error(), "details_len=3")
}

func TestStreamResponse_Close(t *testing.T) {
	t.Parallel()

	nopCloser := io.NopCloser(bytes.NewReader(nil))
	_ = nopCloser
	streamResp := &grpc.StreamResponse[wrapperspb.StringValue]{}
	assert.NoError(t, streamResp.Close())
}
