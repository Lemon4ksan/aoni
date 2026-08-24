// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stream_test

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/realtime/stream"
)

// setupTestServer creates a test server and pre-configures a client with its URL.
// It registers resource cleanup automatically through t.Cleanup.
func setupTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *aoni.Client) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := aoni.NewClient(nil, option.WithBaseURL(server.URL))

	return server, client
}

func TestStream(t *testing.T) {
	t.Parallel()

	t.Run("stream_response_body", func(t *testing.T) {
		t.Parallel()

		_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", "11")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("hello world"))
		})

		s, err := stream.Get(t.Context(), client, "/stream")
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })

		data, err := io.ReadAll(s)
		require.NoError(t, err)

		assert.Equal(t, "hello world", string(data))
		assert.Equal(t, int64(11), s.ContentLength())
		assert.Equal(t, "application/octet-stream", s.ContentType())
		assert.Equal(t, http.StatusOK, s.StatusCode())
	})

	t.Run("stream_error_status", func(t *testing.T) {
		t.Parallel()

		_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})

		_, err := stream.Get(t.Context(), client, "/notfound")
		require.Error(t, err)

		var apiErr *aoni.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	})

	t.Run("stream_with_query_params", func(t *testing.T) {
		t.Parallel()

		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "bar", r.URL.Query().Get("foo"))

			_, _ = w.Write([]byte("ok"))
		})

		query := map[string]string{"foo": "bar"}
		s, err := stream.Get(t.Context(), client, "/test", mod.WithQuery(query))
		require.NoError(t, err)

		t.Cleanup(func() { _ = s.Close() })

		data, err := io.ReadAll(s)
		require.NoError(t, err)
		assert.Equal(t, "ok", string(data))
	})

	t.Run("stream_large_body", func(t *testing.T) {
		t.Parallel()

		largeBody := strings.Repeat("x", 1024*1024)

		_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "1048576")
			_, _ = w.Write([]byte(largeBody))
		})

		s, err := stream.Get(t.Context(), client, "/large")
		require.NoError(t, err)

		t.Cleanup(func() { _ = s.Close() })

		data, err := io.ReadAll(s)
		require.NoError(t, err)
		assert.Equal(t, len(largeBody), len(data))
	})

	t.Run("response_method", func(t *testing.T) {
		t.Parallel()

		_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Custom", "value")
			_, _ = w.Write([]byte("ok"))
		})

		s, err := stream.Get(t.Context(), client, "/test")
		require.NoError(t, err)

		t.Cleanup(func() { _ = s.Close() })

		resp := s.Response()
		assert.Equal(t, "value", resp.Header.Get("X-Custom"))

		_, _ = io.ReadAll(s)
	})
}

func TestStreamWithBody(t *testing.T) {
	t.Parallel()

	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, "post_payload_data", string(body))

		_, _ = w.Write([]byte("response_payload"))
	})

	s, err := stream.WithBody(t.Context(), client, http.MethodPost, "/", strings.NewReader("post_payload_data"))
	require.NoError(t, err)

	t.Cleanup(func() { _ = s.Close() })

	data, err := io.ReadAll(s)
	require.NoError(t, err)
	assert.Equal(t, "response_payload", string(data))
}

func TestStreamNDJSON(t *testing.T) {
	t.Parallel()

	_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message": "msg1"}` + "\n" + `{"message": "msg2"}` + "\n"))
	})

	s, err := stream.Get(t.Context(), client, "/")
	require.NoError(t, err)

	type Msg struct {
		Message string `json:"message"`
	}

	out, errs := stream.GetNDJSON[Msg](t.Context(), s)

	var messages []string
	for msg := range out {
		messages = append(messages, msg.Message)
	}

	for err := range errs {
		require.NoError(t, err)
	}

	assert.Equal(t, []string{"msg1", "msg2"}, messages)
}

func TestStreamNDJSON_ContextCancellation(t *testing.T) {
	t.Parallel()

	_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"message":"first"}` + "\n"))

		time.Sleep(1 * time.Second)

		_, _ = w.Write([]byte(`{"message":"second"}` + "\n"))
	})

	s, err := stream.Get(t.Context(), client, "/")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())

	type Msg struct {
		Message string `json:"message"`
	}

	out, errs := stream.GetNDJSON[Msg](ctx, s)

	msg1 := <-out
	assert.Equal(t, "first", msg1.Message)

	cancel()

	var errList []error
	for err := range errs {
		errList = append(errList, err)
	}

	assert.Contains(t, errList, context.Canceled)
}

func TestStreamSSE(t *testing.T) {
	t.Parallel()

	_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: first\ndata: value1\nid: 1\n\nevent: second\ndata: value2\n\n"))
	})

	s, err := stream.Get(t.Context(), client, "/")
	require.NoError(t, err)

	out, errs := stream.ParseSSE[stream.SSEEvent](t.Context(), s)

	var events []stream.SSEEvent
	for ev := range out {
		events = append(events, ev)
	}

	for err := range errs {
		require.NoError(t, err)
	}

	require.Len(t, events, 2)
	assert.Equal(t, "first", events[0].Event)
	assert.Equal(t, "value1", events[0].Data)
	assert.Equal(t, "1", events[0].ID)

	assert.Equal(t, "second", events[1].Event)
	assert.Equal(t, "value2", events[1].Data)
}

func TestStreamSSE_Integration(t *testing.T) {
	t.Parallel()

	_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: welcome\ndata: joined\n\n"))
	})

	out, errs, err := stream.SSE[stream.SSEEvent](t.Context(), client, "/")
	require.NoError(t, err)

	var events []stream.SSEEvent
	for ev := range out {
		events = append(events, ev)
	}

	for err := range errs {
		require.NoError(t, err)
	}

	require.Len(t, events, 1)
	assert.Equal(t, "welcome", events[0].Event)
	assert.Equal(t, "joined", events[0].Data)
}

func TestResumableSSE_LastEventID(t *testing.T) {
	t.Parallel()

	var (
		receivedLastID string
		attempts       int
	)

	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		receivedLastID = r.Header.Get("Last-Event-ID")
		w.Header().Set("Content-Type", "text/event-stream")

		if attempts == 1 {
			_, _ = w.Write([]byte("id: 42\ndata: event1\nretry: 10\n\n"))
			return
		}

		_, _ = w.Write([]byte("id: 43\ndata: event2\n\n"))
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	events, _, err := stream.ResumableSSE[stream.SSEEvent](
		ctx, client, "/",
		stream.SSEReconnectOptions{DefaultRetry: 10 * time.Millisecond},
	)
	require.NoError(t, err)

	e1 := <-events
	assert.Equal(t, "42", e1.ID)
	assert.Equal(t, "event1", e1.Data)

	e2 := <-events
	assert.Equal(t, "43", e2.ID)
	assert.Equal(t, "event2", e2.Data)
	assert.Equal(t, "42", receivedLastID)
}

func TestResumableSSE_MaxReconnectsExceeded(t *testing.T) {
	t.Parallel()

	_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	_, errs, err := stream.ResumableSSE[stream.SSEEvent](
		ctx, client, "/",
		stream.SSEReconnectOptions{
			MaxReconnects: 2,
			DefaultRetry:  5 * time.Millisecond,
		},
	)
	require.NoError(t, err)

	var errList []error
	for err := range errs {
		errList = append(errList, err)
	}

	require.NotEmpty(t, errList)
	assert.Contains(t, errList[0].Error(), "max reconnect attempts reached")
}

func TestStreamChunks(t *testing.T) {
	t.Parallel()

	_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("token1_token2_token3"))
	})

	s, err := stream.Get(t.Context(), client, "/")
	require.NoError(t, err)

	out, errs := stream.Chunks(t.Context(), s)

	var chunks []string
	for chunk := range out {
		chunks = append(chunks, chunk)
	}

	for err := range errs {
		require.NoError(t, err)
	}

	assert.NotEmpty(t, chunks)
	assert.Equal(t, "token1_token2_token3", strings.Join(chunks, ""))
}

func TestParseGRPCWebStream(t *testing.T) {
	t.Parallel()

	msg1 := wrapperspb.String("grpc_msg_1")
	msg1Bytes, err := proto.Marshal(msg1)
	require.NoError(t, err)

	msg2 := wrapperspb.String("grpc_msg_2")
	msg2Bytes, err := proto.Marshal(msg2)
	require.NoError(t, err)

	_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/grpc-web+proto")

		// Frame 1: Data frame (flags: 0x00) with msg1
		var f1 [5]byte

		f1[0] = 0x00
		binary.BigEndian.PutUint32(f1[1:5], uint32(len(msg1Bytes))) //nolint:gosec
		_, _ = w.Write(f1[:])
		_, _ = w.Write(msg1Bytes)

		// Frame 2: Data frame (flags: 0x00) with msg2
		var f2 [5]byte

		f2[0] = 0x00
		binary.BigEndian.PutUint32(f2[1:5], uint32(len(msg2Bytes))) //nolint:gosec
		_, _ = w.Write(f2[:])
		_, _ = w.Write(msg2Bytes)

		// Frame 3: Trailer frame (flags: 0x80) with grpc-status:0
		trailerText := []byte("grpc-status:0\r\ngrpc-message:OK\r\n")

		var f3 [5]byte

		f3[0] = 0x80
		binary.BigEndian.PutUint32(f3[1:5], uint32(len(trailerText))) //nolint:gosec
		_, _ = w.Write(f3[:])
		_, _ = w.Write(trailerText)
	})

	s, err := stream.Get(t.Context(), client, "/")
	require.NoError(t, err)

	out, errs := stream.ParseGRPCWebStream[*wrapperspb.StringValue](t.Context(), s)

	var msgs []string
	for val := range out {
		msgs = append(msgs, val.GetValue())
	}

	for err := range errs {
		require.NoError(t, err)
	}

	assert.Equal(t, []string{"grpc_msg_1", "grpc_msg_2"}, msgs)
}

func TestStreamSSE_DoneAndIndentation(t *testing.T) {
	t.Parallel()

	t.Run("llm_done_signal", func(t *testing.T) {
		t.Parallel()

		_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"text\": \"hello\"}\n\ndata: [DONE]\n\n"))
		})

		type Chunk struct {
			Text string `json:"text"`
		}

		out, errs, err := stream.SSE[Chunk](t.Context(), client, "/")
		require.NoError(t, err)

		var chunks []Chunk
		for c := range out {
			chunks = append(chunks, c)
		}

		for err := range errs {
			require.NoError(t, err)
		}

		require.Len(t, chunks, 1)
		assert.Equal(t, "hello", chunks[0].Text)
	})

	t.Run("indentation_preservation", func(t *testing.T) {
		t.Parallel()

		_, client := setupTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data:   def foo():\ndata:       return 42\n\n"))
		})

		out, errs, err := stream.SSE[stream.SSEEvent](t.Context(), client, "/")
		require.NoError(t, err)

		var events []stream.SSEEvent
		for ev := range out {
			events = append(events, ev)
		}

		for err := range errs {
			require.NoError(t, err)
		}

		require.Len(t, events, 1)
		assert.Equal(t, "  def foo():\n      return 42", events[0].Data)
	})

	t.Run("NDJSONReader_Sequential", func(t *testing.T) {
		t.Parallel()

		type Msg struct {
			Text string `json:"text"`
		}

		r := io.NopCloser(strings.NewReader("{\"text\":\"first\"}\n{\"text\":\"second\"}\n"))
		ndjson := stream.NewNDJSONReader[Msg](r)
		t.Cleanup(func() { _ = ndjson.Close() })

		res1 := ndjson.Next()
		require.True(t, res1.IsSuccess())
		val1, err := res1.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, "first", val1.Text)

		res2 := ndjson.Next()
		require.True(t, res2.IsSuccess())
		val2, err := res2.Unwrap()
		require.NoError(t, err)
		assert.Equal(t, "second", val2.Text)

		res3 := ndjson.Next()
		assert.False(t, res3.IsSuccess())
	})

	t.Run("SSEReader_Iterator_And_All", func(t *testing.T) {
		t.Parallel()

		payload := "event: update\ndata: {\"text\":\"chunk1\"}\n\nevent: update\ndata: {\"text\":\"chunk2\"}\n\n"
		r := io.NopCloser(strings.NewReader(payload))
		sseReader := stream.NewSSEReader[struct {
			Text string `json:"text"`
		}](r)
		t.Cleanup(func() { _ = sseReader.Close() })

		var results []string
		for chunk, err := range sseReader.All() {
			require.NoError(t, err)

			results = append(results, chunk.Text)
		}

		assert.Equal(t, []string{"chunk1", "chunk2"}, results)
	})

	t.Run("NDJSONReader_Iterator_And_All", func(t *testing.T) {
		t.Parallel()

		type Msg struct {
			Text string `json:"text"`
		}

		r := io.NopCloser(strings.NewReader("{\"text\":\"item1\"}\n{\"text\":\"item2\"}\n"))
		ndjson := stream.NewNDJSONReader[Msg](r)
		t.Cleanup(func() { _ = ndjson.Close() })

		var results []string
		for msg, err := range ndjson.All() {
			require.NoError(t, err)

			results = append(results, msg.Text)
		}

		assert.Equal(t, []string{"item1", "item2"}, results)
	})

	t.Run("ChunkReader_Iterator_And_All", func(t *testing.T) {
		t.Parallel()

		r := io.NopCloser(strings.NewReader("hello world from aoni chunk reader"))
		chunkReader := stream.NewChunkReader(r, 8)
		t.Cleanup(func() { _ = chunkReader.Close() })

		var chunks []string
		for chunk, err := range chunkReader.All() {
			require.NoError(t, err)

			chunks = append(chunks, string(chunk))
		}

		assert.Equal(t, "hello world from aoni chunk reader", strings.Join(chunks, ""))
	})
}
