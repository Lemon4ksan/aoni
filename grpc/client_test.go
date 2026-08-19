// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package grpc_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/lemon4ksan/aoni"
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
		frame, err := grpc.MarshalFrame(msg, true)
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

	streamResp := &grpc.StreamResponse[wrapperspb.StringValue]{}
	assert.NoError(t, streamResp.Close())
}

func TestBidiStream_Lifecycle(t *testing.T) {
	t.Parallel()

	bidi := &grpc.BidiStreamClient[*wrapperspb.StringValue, wrapperspb.StringValue]{}
	assert.NoError(t, bidi.Close())
}

func TestClientStream_Lifecycle(t *testing.T) {
	t.Parallel()

	clientStream := &grpc.ClientStreamClient[*wrapperspb.StringValue, wrapperspb.StringValue]{}
	assert.NoError(t, clientStream.Close())
}

func TestServerStream_FunctionalRoundtrip(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Trailer", "grpc-status, grpc-message")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)

		// Send 3 streaming frames
		for i := 1; i <= 3; i++ {
			msg := wrapperspb.Int32(int32(i * 10))

			frame, err := grpc.MarshalFrame(msg, false)
			if err != nil {
				return
			}

			_, _ = w.Write(frame)

			if flusher != nil {
				flusher.Flush()
			}
		}

		w.Header().Set("grpc-status", "0")
		w.Header().Set("grpc-message", "OK")
	}))
	defer server.Close()

	ctx := context.Background()
	client := aoni.NewClient(nil)

	stream, err := grpc.ServerStream[wrapperspb.Int32Value](
		ctx,
		client,
		server.URL+"/TestService/StreamInts",
		wrapperspb.String("start"),
	)
	require.NoError(t, err)

	defer stream.Close()

	assert.NotNil(t, stream.Header())

	var values []int32
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)

		values = append(values, msg.GetValue())
	}

	assert.Equal(t, []int32{10, 20, 30}, values)
	assert.NotNil(t, stream.Trailer())
}

func TestBidiStream_FunctionalRoundtrip(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		_ = rc.EnableFullDuplex()

		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Trailer", "grpc-status, grpc-message")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)

		// Read incoming frames and echo back multiplied by 2
		for {
			var incoming wrapperspb.Int32Value

			_, err := grpc.UnmarshalFrame(r.Body, &incoming)
			if err != nil {
				break
			}

			outgoing := wrapperspb.Int32(incoming.GetValue() * 2)

			frame, err := grpc.MarshalFrame(outgoing, false)
			if err != nil {
				break
			}

			_, _ = w.Write(frame)

			if flusher != nil {
				flusher.Flush()
			}
		}

		w.Header().Set("grpc-status", "0")
		w.Header().Set("grpc-message", "OK")
	}))
	defer server.Close()

	ctx := context.Background()
	client := aoni.NewClient(nil)

	bidi, err := grpc.BidiStream[*wrapperspb.Int32Value, wrapperspb.Int32Value](
		ctx,
		client,
		server.URL+"/TestService/EchoMultiplied",
	)
	require.NoError(t, err)

	defer bidi.Close()

	var results []int32

	go func() {
		for i := int32(1); i <= 3; i++ {
			if err := bidi.Send(wrapperspb.Int32(i)); err != nil {
				t.Logf("client send error at %d: %v", i, err)
			}
		}

		_ = bidi.CloseSend()
	}()

	for {
		msg, err := bidi.Recv()
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)

		results = append(results, msg.GetValue())
	}

	assert.Equal(t, []int32{2, 4, 6}, results)
	assert.NotNil(t, bidi.Trailer())
}

func TestClientStream_FunctionalRoundtrip(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Trailer", "grpc-status, grpc-message")
		w.WriteHeader(http.StatusOK)

		var total int32
		for {
			var incoming wrapperspb.Int32Value

			_, err := grpc.UnmarshalFrame(r.Body, &incoming)
			if err != nil {
				break
			}

			total += incoming.GetValue()
		}

		outgoing := wrapperspb.Int32(total)
		frame, _ := grpc.MarshalFrame(outgoing, false)
		_, _ = w.Write(frame)

		w.Header().Set("grpc-status", "0")
		w.Header().Set("grpc-message", "OK")
	}))
	defer server.Close()

	ctx := context.Background()
	client := aoni.NewClient(nil)

	clientStream, err := grpc.ClientStream[*wrapperspb.Int32Value, wrapperspb.Int32Value](
		ctx,
		client,
		server.URL+"/TestService/SumAll",
	)
	require.NoError(t, err)

	defer clientStream.Close()

	// Send 5, 10, 15
	require.NoError(t, clientStream.Send(wrapperspb.Int32(5)))
	require.NoError(t, clientStream.Send(wrapperspb.Int32(10)))
	require.NoError(t, clientStream.Send(wrapperspb.Int32(15)))

	resp, err := clientStream.CloseAndRecv()
	require.NoError(t, err)
	assert.Equal(t, int32(30), resp.GetValue())
}
