// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStream(t *testing.T) {
	t.Parallel()

	t.Run("stream_response_body", func(t *testing.T) {
		t.Parallel()
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", "11")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("hello world"))
		})

		stream, err := Stream(t.Context(), client, "/stream")
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		data, err := io.ReadAll(stream)
		require.NoError(t, err)
		assert.Equal(t, "hello world", string(data))
		assert.Equal(t, int64(11), stream.ContentLength())
		assert.Equal(t, "application/octet-stream", stream.ContentType())
		assert.Equal(t, http.StatusOK, stream.StatusCode())
	})

	t.Run("stream_error_status", func(t *testing.T) {
		t.Parallel()
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})

		_, err := Stream(t.Context(), client, "/notfound")
		require.Error(t, err)

		var apiErr *APIError
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
		stream, err := Stream(t.Context(), client, "/test", func(req *http.Request) {
			q := req.URL.Query()
			for k, v := range query {
				q.Set(k, v)
			}

			req.URL.RawQuery = q.Encode()
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		data, err := io.ReadAll(stream)
		require.NoError(t, err)
		assert.Equal(t, "ok", string(data))
	})

	t.Run("stream_with_request_modifier", func(t *testing.T) {
		t.Parallel()
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))

			_, _ = w.Write([]byte("authorized"))
		})

		stream, err := Stream(t.Context(), client, "/auth", WithBearer("token123"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		data, err := io.ReadAll(stream)
		require.NoError(t, err)
		assert.Equal(t, "authorized", string(data))
	})

	t.Run("stream_large_body", func(t *testing.T) {
		t.Parallel()

		largeBody := strings.Repeat("x", 1024*1024)

		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "1048576")
			_, _ = w.Write([]byte(largeBody))
		})

		stream, err := Stream(t.Context(), client, "/large")
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		data, err := io.ReadAll(stream)
		require.NoError(t, err)
		assert.Equal(t, len(largeBody), len(data))
	})

	t.Run("response_method", func(t *testing.T) {
		t.Parallel()
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Custom", "value")
			_, _ = w.Write([]byte("ok"))
		})

		stream, err := Stream(t.Context(), client, "/test")
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close() })

		resp := stream.Response()
		assert.Equal(t, "value", resp.Header.Get("X-Custom"))

		_, _ = io.ReadAll(stream)
	})
}

func TestStreamNDJSON(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"message": "msg1"}` + "\n" + `{"message": "msg2"}` + "\n"))
	})

	stream, err := Stream(t.Context(), client, "/")
	require.NoError(t, err)

	type Msg struct {
		Message string `json:"message"`
	}

	out, errs := StreamNDJSON[Msg](t.Context(), stream)

	var messages []string
	for msg := range out {
		messages = append(messages, msg.Message)
	}

	for err := range errs {
		require.NoError(t, err)
	}

	assert.Equal(t, []string{"msg1", "msg2"}, messages)
}

func TestStreamSSE(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: first\ndata: value1\nid: 1\n\nevent: second\ndata: value2\n\n"))
	})

	stream, err := Stream(t.Context(), client, "/")
	require.NoError(t, err)

	out, errs := ParseSSE[SSEEvent](t.Context(), stream)

	var events []SSEEvent
	for ev := range out {
		events = append(events, ev)
	}

	require.NoError(t, err)

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
