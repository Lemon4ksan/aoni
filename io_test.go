// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	pr := &progressReader{
		reader: inner,
		total:  10,
		onProgress: func(current, total int64) {
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

	cc := &contextCancelingReadCloser{
		ReadCloser: inner,
		cancel:     cancel,
	}

	assert.NoError(t, ctx.Err())

	_ = cc.Close()

	assert.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestDecompressReadCloser(t *testing.T) {
	t.Parallel()

	m1 := &mockTrackedCloser{Reader: strings.NewReader("abc")}
	m2 := &mockTrackedCloser{Reader: strings.NewReader("def")}

	dec := &decompressReadCloser{
		Reader: m1,
		closer: m2,
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
		lc := &limitCheckingReadCloser{ReadCloser: inner, limit: 5}

		buf := make([]byte, 10)
		n, err := lc.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, 3, n)
	})

	t.Run("exceeds_limit", func(t *testing.T) {
		t.Parallel()

		inner := io.NopCloser(strings.NewReader("abcdefg")) // 7 bytes
		lc := &limitCheckingReadCloser{ReadCloser: inner, limit: 5}

		buf := make([]byte, 10)
		_, err := lc.Read(buf)
		assert.ErrorIs(t, err, ErrResponseTooLarge)
	})
}

func TestFinalizerReadCloser_GC(t *testing.T) {
	t.Parallel()

	closedCh := make(chan bool, 1)
	m := &mockTrackedCloser{Reader: strings.NewReader("gc_test")}

	// We run allocation in an isolated helper to prevent compiler escape analysis
	// keeping the reference alive on the stack.
	func() {
		f := newFinalizerReadCloser(m)
		_ = f
	}()

	// Trigger GC to run finalizer
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	runtime.GC()

	// Normal manual Close should remove the finalizer safely
	m2 := &mockTrackedCloser{Reader: strings.NewReader("normal")}
	f2 := newFinalizerReadCloser(m2)
	_ = f2.Close()

	assert.True(t, m2.closed)

	_ = closedCh
}

func TestBOMStrippingReader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{"utf8_bom", []byte{0xEF, 0xBB, 0xBF, 'a', 'b'}, []byte("ab")},
		{"utf16be_bom", []byte{0xFE, 0xFF, 'x', 'y'}, []byte("xy")},
		{"utf16le_bom", []byte{0xFF, 0xFE, 'z', 'w'}, []byte("zw")},
		{"no_bom", []byte("standard"), []byte("standard")},
		{"short_input", []byte("a"), []byte("a")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := newBOMStrippingReader(bytes.NewReader(tt.input))
			out, err := io.ReadAll(reader)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, out)
		})
	}
}

func TestMultiReadBody(t *testing.T) {
	t.Parallel()

	t.Run("under_threshold_memory_buffered", func(t *testing.T) {
		t.Parallel()

		inner := io.NopCloser(strings.NewReader("under_threshold"))
		m, err := newMultiReadBody(inner, 50)
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
		m, err := newMultiReadBody(inner, 10)
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
		_, err := newMultiReadBody(errReader, 50)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})
}
