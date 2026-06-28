// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestMultiReadBody_FileCleanup(t *testing.T) {
	t.Parallel()

	data := strings.Repeat("x", 64*1024)

	body := io.NopCloser(strings.NewReader(data))
	mrb, err := newMultiReadBody(body, 32*1024)
	require.NoError(t, err)

	mrc := mrb.(*multiReadBody)
	require.NotNil(t, mrc.tmpFile)

	tmpPath := mrc.tmpFile.Name()

	buf := make([]byte, len(data))
	n, err := io.ReadFull(mrc, buf)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)

	err = mrc.Close()
	require.NoError(t, err)

	_, err = os.Stat(tmpPath)
	assert.NoError(t, err)

	mrc.ReallyClose()

	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err))
}

func TestFinalizerReadCloser_CallsReallyClose(t *testing.T) {
	t.Parallel()

	data := strings.Repeat("y", 64*1024)

	body := io.NopCloser(strings.NewReader(data))
	mrb, err := newMultiReadBody(body, 32*1024)
	require.NoError(t, err)

	mrc := mrb.(*multiReadBody)
	require.NotNil(t, mrc.tmpFile)
	tmpPath := mrc.tmpFile.Name()

	frc := newFinalizerReadCloser(mrb)

	err = frc.Close()
	require.NoError(t, err)

	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err))
}

func TestMultiReadBody_InMemory_NoTmpFile(t *testing.T) {
	t.Parallel()

	data := "small data"
	body := io.NopCloser(strings.NewReader(data))
	mrb, err := newMultiReadBody(body, 1024)
	require.NoError(t, err)

	mrc := mrb.(*multiReadBody)
	assert.Nil(t, mrc.tmpFile)

	buf, err := io.ReadAll(mrc)
	require.NoError(t, err)
	assert.Equal(t, data, string(buf))

	err = mrc.Close()
	require.NoError(t, err)

	mrc.ReallyClose()
}

func TestProgressReader_AtomicIncrement(t *testing.T) {
	t.Parallel()

	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	var (
		totalRead int64
		mu        sync.Mutex
	)

	seen := make(map[int64]bool)

	pr := &progressReader{
		reader: bytes.NewReader(data),
		total:  int64(len(data)),
		onProgress: func(current, total int64) {
			mu.Lock()
			seen[current] = true
			totalRead = current
			mu.Unlock()
		},
	}

	buf := make([]byte, 256)
	for {
		n, err := pr.Read(buf)
		if err != nil {
			break
		}

		_ = n
	}

	mu.Lock()
	assert.Equal(t, int64(len(data)), totalRead)
	assert.True(t, len(seen) > 0)
	mu.Unlock()
}

func TestProgressReader_ConcurrentSafety(t *testing.T) {
	t.Parallel()

	pr := &progressReader{
		reader:     &threadSafeReader{data: make([]byte, 4096)},
		total:      4096,
		onProgress: func(_, _ int64) {},
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			buf := make([]byte, 64)
			for {
				_, err := pr.Read(buf)
				if err != nil {
					return
				}
			}
		})
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for concurrent reads")
	}
}

type threadSafeReader struct {
	mu   sync.Mutex
	data []byte
	pos  int
}

func (r *threadSafeReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.pos >= len(r.data) {
		return 0, io.EOF
	}

	n := copy(p, r.data[r.pos:])
	r.pos += n

	return n, nil
}

func TestMultiReadBody_GC_FinalizerCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping GC test in short mode")
	}

	data := strings.Repeat("z", 64*1024)

	for range 5 {
		body := io.NopCloser(strings.NewReader(data))
		mrb, _ := newMultiReadBody(body, 32*1024)
		mrc := mrb.(*multiReadBody)

		buf := make([]byte, 1024)
		_, _ = mrc.Read(buf)

		_ = mrc
	}

	runtime.GC()
	runtime.Gosched()
	time.Sleep(100 * time.Millisecond)
}

func TestStreamWithBody(t *testing.T) {
	t.Parallel()
	_, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, "post_payload_data", string(body))

		_, _ = w.Write([]byte("response_payload"))
	})

	stream, err := StreamWithBody(t.Context(), client, http.MethodPost, "/", strings.NewReader("post_payload_data"))
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

	out, errs, err := StreamSSE[SSEEvent](t.Context(), client, "/")
	require.NoError(t, err)

	var events []SSEEvent
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

	stream, err := Stream(t.Context(), client, "/")
	require.NoError(t, err)

	out, errs := StreamChunks(t.Context(), stream)

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

	stream, err := Stream(t.Context(), client, "/")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())

	type Msg struct {
		Message string `json:"message"`
	}

	out, errs := StreamNDJSON[Msg](ctx, stream)

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
