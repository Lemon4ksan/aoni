// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package io

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

type customTestCloser struct {
	io.Closer
	marker int
}

type mockTrackedCloser struct {
	io.Reader
	closed bool
}

func (m *mockTrackedCloser) Close() error {
	m.closed = true
	return nil
}

type ioErrorReader struct {
	data []byte
	err  error
}

func (r *ioErrorReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}

	return 0, r.err
}

func (r *ioErrorReader) Close() error {
	return nil
}

func TestUnwrapTo(t *testing.T) {
	t.Parallel()

	inner := io.NopCloser(strings.NewReader("test"))
	wrapped := &customTestCloser{Closer: inner, marker: 999}

	target, ok := UnwrapTo[*customTestCloser](wrapped)
	assert.True(t, ok)
	assert.Equal(t, 999, target.marker)

	_, ok = UnwrapTo[string](wrapped)
	assert.False(t, ok)
}

func TestAsReplayable_Operations(t *testing.T) {
	t.Parallel()

	t.Run("nil_input", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, AsReplayable(nil))
	})

	t.Run("wrap_and_read_multiple_times", func(t *testing.T) {
		t.Parallel()

		inner := io.NopCloser(strings.NewReader("hello world"))
		rep := AsReplayable(inner)
		require.NotNil(t, rep)

		// First read
		b1, err := io.ReadAll(rep)
		require.NoError(t, err)
		assert.Equal(t, "hello world", string(b1))

		// Reset and read again
		rep.Reset()
		b2, err := io.ReadAll(rep)
		require.NoError(t, err)
		assert.Equal(t, "hello world", string(b2))
	})

	t.Run("unwrap_already_replayable", func(t *testing.T) {
		t.Parallel()

		inner := io.NopCloser(strings.NewReader("data"))
		rep1 := AsReplayable(inner)

		// Wrapping a replayable should return the same replayable
		rep2 := AsReplayable(rep1)
		assert.Equal(t, rep1, rep2)
	})
}

func TestReadAllHelpers(t *testing.T) {
	t.Parallel()

	t.Run("read_all_string_and_bytes", func(t *testing.T) {
		t.Parallel()

		inner := io.NopCloser(strings.NewReader("reusable stream content"))
		rep := AsReplayable(inner)

		str, err := ReadAllString(rep)
		require.NoError(t, err)
		assert.Equal(t, "reusable stream content", str)

		// Helpers must call Reset() internally, so we can immediately read bytes
		data, err := ReadAllBytes(rep)
		require.NoError(t, err)
		assert.Equal(t, []byte("reusable stream content"), data)
	})

	t.Run("helpers_with_nil_input", func(t *testing.T) {
		t.Parallel()

		str, err := ReadAllString(nil)
		require.NoError(t, err)
		assert.Empty(t, str)

		data, err := ReadAllBytes(nil)
		require.NoError(t, err)
		assert.Nil(t, data)
	})
}

func TestProgressReader(t *testing.T) {
	t.Parallel()

	inner := io.NopCloser(strings.NewReader("abcdefghij")) // 10 bytes

	var (
		lastCurrent int64
		lastTotal   int64
	)

	pr := &ProgressReader{
		Reader: inner,
		Total:  10,
		OnProgress: func(current, total int64) {
			lastCurrent = current
			lastTotal = total
		},
	}

	buf := make([]byte, 4)
	n, err := pr.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, int64(4), lastCurrent)
	assert.Equal(t, int64(10), lastTotal)

	_ = pr.Close()
}

func TestContextCancelingReadCloser(t *testing.T) {
	t.Parallel()

	inner := io.NopCloser(strings.NewReader("test"))
	ctx, cancel := context.WithCancel(t.Context())

	cc := &ContextCancelingReadCloser{
		ReadCloser: inner,
		Cancel:     cancel,
	}

	assert.NoError(t, ctx.Err())

	_ = cc.Close()

	assert.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestDecompressReadCloser(t *testing.T) {
	t.Parallel()

	m1 := &mockTrackedCloser{Reader: strings.NewReader("abc")}
	m2 := &mockTrackedCloser{Reader: strings.NewReader("def")}

	dec := &DecompressReadCloser{
		Reader: m1,
		Closer: m2,
	}

	err := dec.Close()
	require.NoError(t, err)
	assert.True(t, m1.closed)
	assert.True(t, m2.closed)
}

func TestLimitCheckingReadCloser(t *testing.T) {
	t.Parallel()

	t.Run("within_limit", func(t *testing.T) {
		t.Parallel()

		inner := io.NopCloser(strings.NewReader("abc"))
		lc := &LimitCheckingReadCloser{ReadCloser: inner, Limit: 5}

		buf := make([]byte, 10)
		n, err := lc.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, 3, n)
	})

	t.Run("exceeds_limit", func(t *testing.T) {
		t.Parallel()

		inner := io.NopCloser(strings.NewReader("abcdefg")) // 7 bytes
		lc := &LimitCheckingReadCloser{ReadCloser: inner, Limit: 5}

		buf := make([]byte, 10)
		_, err := lc.Read(buf)
		assert.ErrorIs(t, err, ErrResponseTooLarge)
	})
}

func TestMultiReadBody(t *testing.T) {
	t.Parallel()

	t.Run("under_threshold_memory_buffered", func(t *testing.T) {
		t.Parallel()

		inner := io.NopCloser(strings.NewReader("under_threshold"))
		m, err := NewMultiReadBody(inner, 50, false)
		require.NoError(t, err)

		// Read first time
		b1, _ := io.ReadAll(m)
		assert.Equal(t, "under_threshold", string(b1))

		// Close / Reset and read again
		_ = m.Close()
		b2, _ := io.ReadAll(m)
		assert.Equal(t, "under_threshold", string(b2))

		// Test ReallyClose is safe
		if really, ok := m.(interface{ ReallyClose() }); ok {
			really.ReallyClose()
		}
	})

	t.Run("above_threshold_temp_file", func(t *testing.T) {
		t.Parallel()

		inner := io.NopCloser(strings.NewReader("above_threshold_large_content_stream"))
		m, err := NewMultiReadBody(inner, 10, false)
		require.NoError(t, err)

		// Read first time
		b1, _ := io.ReadAll(m)
		assert.Equal(t, "above_threshold_large_content_stream", string(b1))

		// Close / Reset and read again
		_ = m.Close()
		b2, _ := io.ReadAll(m)
		assert.Equal(t, "above_threshold_large_content_stream", string(b2))

		// Clean up file via ReallyClose
		if really, ok := m.(interface{ ReallyClose() }); ok {
			really.ReallyClose()
		}
	})

	t.Run("read_buffering_error", func(t *testing.T) {
		t.Parallel()

		errReader := &ioErrorReader{data: []byte("partial"), err: io.ErrUnexpectedEOF}
		_, err := NewMultiReadBody(errReader, 50, false)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	t.Run("disable_disk_cache_error", func(t *testing.T) {
		t.Parallel()

		inner := io.NopCloser(strings.NewReader("above_threshold_large_content_stream"))
		_, err := NewMultiReadBody(inner, 10, true)
		assert.ErrorIs(t, err, ErrBufferLimitExceeded)
	})
}

func TestMultiReadBody_FileCleanup(t *testing.T) {
	t.Parallel()

	data := strings.Repeat("x", 64*1024)

	body := io.NopCloser(strings.NewReader(data))
	mrb, err := NewMultiReadBody(body, 32*1024, false)
	require.NoError(t, err)

	mrc := mrb.(*MultiReadBody)
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

func TestResponseBodyReadCloser_CallsReallyClose(t *testing.T) {
	t.Parallel()

	data := strings.Repeat("y", 64*1024)

	body := io.NopCloser(strings.NewReader(data))
	mrb, err := NewMultiReadBody(body, 32*1024, false)
	require.NoError(t, err)

	mrc := mrb.(*MultiReadBody)
	require.NotNil(t, mrc.tmpFile)
	tmpPath := mrc.tmpFile.Name()

	frc := ResponseBodyReadCloser{ReadCloser: mrb}

	err = frc.Close()
	require.NoError(t, err)

	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err))
}

func TestMultiReadBody_InMemory_NoTmpFile(t *testing.T) {
	t.Parallel()

	data := "small data"
	body := io.NopCloser(strings.NewReader(data))
	mrb, err := NewMultiReadBody(body, 1024, false)
	require.NoError(t, err)

	mrc := mrb.(*MultiReadBody)
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

	pr := &ProgressReader{
		Reader: bytes.NewReader(data),
		Total:  int64(len(data)),
		OnProgress: func(current, total int64) {
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

	pr := &ProgressReader{
		Reader:     &threadSafeReader{data: make([]byte, 4096)},
		Total:      4096,
		OnProgress: func(_, _ int64) {},
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

func TestMultiReadBody_DoubleCloseIdempotency(t *testing.T) {
	t.Parallel()

	rc := io.NopCloser(strings.NewReader("payload to write onto temp file in disk storage"))
	mBody, err := NewMultiReadBody(rc, 5, false)
	require.NoError(t, err)

	underlying, ok := mBody.(*MultiReadBody)
	require.True(t, ok)
	require.NotNil(t, underlying.tmpFile)

	tempPath := underlying.tmpFile.Name()

	_, err = os.Stat(tempPath)
	require.NoError(t, err)

	underlying.ReallyClose()
	underlying.ReallyClose()

	_, err = os.Stat(tempPath)
	assert.True(t, os.IsNotExist(err), "expected temporary file to be fully removed from disk after closed")
}
