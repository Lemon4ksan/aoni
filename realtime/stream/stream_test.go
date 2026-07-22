// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stream_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/realtime/stream"
)

// setupTestServer creates a test server and pre-configures a client With its URL.
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
		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", "11")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("hello world"))
		})

		stream, err := stream.Get(t.Context(), client, "/stream")
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
		stream, err := stream.Get(t.Context(), client, "/test", func(req *http.Request) {
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

	t.Run("stream_large_body", func(t *testing.T) {
		t.Parallel()

		largeBody := strings.Repeat("x", 1024*1024)

		_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "1048576")
			_, _ = w.Write([]byte(largeBody))
		})

		stream, err := stream.Get(t.Context(), client, "/large")
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

		stream, err := stream.Get(t.Context(), client, "/test")
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

func TestStreamSSE(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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

func TestStreamWithBody(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, "post_payload_data", string(body))

		_, _ = w.Write([]byte("response_payload"))
	})

	stream, err := stream.WithBody(t.Context(), client, http.MethodPost, "/", strings.NewReader("post_payload_data"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	data, err := io.ReadAll(stream)
	require.NoError(t, err)
	assert.Equal(t, "response_payload", string(data))
}

func TestStreamSSE_Integration(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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

func TestStreamChunks(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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

func TestStreamNDJSON_ContextCancellation(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Send first record, wait/block, then send second
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

	// Consume the first available message
	msg1 := <-out
	assert.Equal(t, "first", msg1.Message)

	// Instantly cancel context to interrupt background reader goroutine
	cancel()

	var errList []error
	for err := range errs {
		errList = append(errList, err)
	}

	assert.Contains(t, errList, context.Canceled)
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
			// Disconnect abruptly without EOF
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
