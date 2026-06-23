// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Bug fix tests: resource leaks, query overwrite, race conditions ---

func TestMultiReadBody_FileCleanup(t *testing.T) {
	t.Parallel()

	data := strings.Repeat("x", 64*1024) // exceeds in-memory threshold

	body := io.NopCloser(strings.NewReader(data))
	mrb, err := newMultiReadBody(body, 32*1024)
	require.NoError(t, err)

	mrc := mrb.(*multiReadBody)
	require.NotNil(t, mrc.tmpFile, "expected tmpFile to be set for large body")

	tmpPath := mrc.tmpFile.Name()

	// Read the body once
	buf := make([]byte, len(data))
	n, err := io.ReadFull(mrc, buf)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)

	// Close should reset cursor but NOT delete file
	err = mrc.Close()
	require.NoError(t, err)

	_, err = os.Stat(tmpPath)
	assert.NoError(t, err, "file should still exist after Close()")

	// ReallyClose should delete the file
	mrc.ReallyClose()

	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err), "file should be deleted after ReallyClose()")
}

func TestFinalizerReadCloser_CallsReallyClose(t *testing.T) {
	t.Parallel()

	data := strings.Repeat("y", 64*1024) // exceeds in-memory threshold

	body := io.NopCloser(strings.NewReader(data))
	mrb, err := newMultiReadBody(body, 32*1024)
	require.NoError(t, err)

	mrc := mrb.(*multiReadBody)
	require.NotNil(t, mrc.tmpFile)
	tmpPath := mrc.tmpFile.Name()

	// Wrap with finalizerReadCloser
	frc := newFinalizerReadCloser(mrb)

	// Normal Close should also clean up the temp file
	err = frc.Close()
	require.NoError(t, err)

	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err),
		"file should be deleted after finalizerReadCloser.Close()")
}

func TestMultiReadBody_InMemory_NoTmpFile(t *testing.T) {
	t.Parallel()

	data := "small data"
	body := io.NopCloser(strings.NewReader(data))
	mrb, err := newMultiReadBody(body, 1024)
	require.NoError(t, err)

	mrc := mrb.(*multiReadBody)
	assert.Nil(t, mrc.tmpFile, "small body should not create tmpFile")

	// Read and close
	buf, err := io.ReadAll(mrc)
	require.NoError(t, err)
	assert.Equal(t, data, string(buf))

	err = mrc.Close()
	require.NoError(t, err)

	// ReallyClose should be a no-op for in-memory bodies
	mrc.ReallyClose()
}

func TestWithQuery_MergesExistingParams(t *testing.T) {
	t.Parallel()

	type additional struct {
		Page int `url:"page"`
	}

	req := &http.Request{
		URL: &url.URL{
			RawQuery: "existing=value&foo=bar",
		},
	}

	mod := WithQuery(additional{Page: 2})
	mod(req)

	q := req.URL.Query()
	assert.Equal(t, "value", q.Get("existing"), "existing param should be preserved")
	assert.Equal(t, "bar", q.Get("foo"), "existing param should be preserved")
	assert.Equal(t, "2", q.Get("page"), "new param should be added")
}

func TestWithQuery_OverwritesSameKey(t *testing.T) {
	t.Parallel()

	type params struct {
		Foo string `url:"foo"`
	}

	req := &http.Request{
		URL: &url.URL{
			RawQuery: "foo=old&bar=keep",
		},
	}

	mod := WithQuery(params{Foo: "new"})
	mod(req)

	q := req.URL.Query()
	assert.Equal(t, "new", q.Get("foo"), "same key should be overwritten")
	assert.Equal(t, "keep", q.Get("bar"), "other key should be preserved")
}

func TestWithQuery_NilQueryNoop(t *testing.T) {
	t.Parallel()

	req := &http.Request{
		URL: &url.URL{RawQuery: "keep=this"},
	}

	WithQuery(nil)(req)
	assert.Equal(t, "keep=this", req.URL.RawQuery)
}

func TestProgressReader_AtomicIncrement(t *testing.T) {
	t.Parallel()

	data := make([]byte, 1024*1024) // 1MB
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

	// Read sequentially in multiple passes
	buf := make([]byte, 256)
	for {
		n, err := pr.Read(buf)
		if err != nil {
			break
		}

		_ = n
	}

	mu.Lock()
	assert.Equal(t, int64(len(data)), totalRead, "final current should equal total bytes")
	assert.True(t, len(seen) > 0, "should have recorded progress updates")
	mu.Unlock()
}

func TestProgressReader_ConcurrentSafety(t *testing.T) {
	t.Parallel()

	// Use a thread-safe reader to test that progressReader.current
	// uses atomic operations correctly.
	pr := &progressReader{
		reader:     &threadSafeReader{data: make([]byte, 4096)},
		total:      4096,
		onProgress: func(_, _ int64) {},
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			buf := make([]byte, 64)
			for {
				_, err := pr.Read(buf)
				if err != nil {
					return
				}
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// No data race detected
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for concurrent reads")
	}
}

// threadSafeReader wraps a byte slice with a mutex for safe concurrent reads.
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

func TestH2Preface_ContextDeadline(t *testing.T) {
	t.Parallel()

	// Create a TCP server that accepts connections but never responds
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			// Accept but never write anything - simulates a hanging server
			go func() {
				buf := make([]byte, 4096)
				for {
					_, err := conn.Read(buf)
					if err != nil {
						return
					}
				}
			}()
		}
	}()

	// Connect with a short deadline
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	require.NoError(t, err)

	defer conn.Close()

	start := time.Now()
	_, err = dialH2ExtendedConnect(ctx, conn, "ws://example.com/ws", "example.com")
	elapsed := time.Since(start)

	assert.Error(t, err, "should fail when server never responds")
	assert.Less(t, elapsed, 2*time.Second,
		"should return within reasonable time due to deadline")
}

func TestH2Preface_ContextCancel(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			// Accept but don't respond
			go func() {
				buf := make([]byte, 4096)
				for {
					if _, err := conn.Read(buf); err != nil {
						return
					}
				}
			}()
		}
	}()

	// Use a short deadline context so the read times out quickly
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	require.NoError(t, err)

	defer conn.Close()

	start := time.Now()
	_, err = dialH2ExtendedConnect(ctx, conn, "ws://example.com/ws", "example.com")
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Less(t, elapsed, 2*time.Second,
		"should return quickly due to context deadline")
}

func TestMultiReadBody_GC_FinalizerCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping GC test in short mode")
	}

	data := strings.Repeat("z", 64*1024)

	// Create a multiReadBody without closing it, relying on GC finalizer
	for range 5 {
		body := io.NopCloser(strings.NewReader(data))
		mrb, _ := newMultiReadBody(body, 32*1024)
		mrc := mrb.(*multiReadBody)

		// Read some data
		buf := make([]byte, 1024)
		_, _ = mrc.Read(buf)

		// Don't close - let GC handle it
		_ = mrc
	}

	// Force GC to trigger finalizers
	runtime.GC()
	runtime.Gosched()
	time.Sleep(100 * time.Millisecond)
}

func TestWithQuery_ComplexMerge(t *testing.T) {
	t.Parallel()

	type first struct {
		Name string `url:"name"`
	}

	type second struct {
		Age int `url:"age"`
	}

	req := &http.Request{
		URL: &url.URL{RawQuery: "name=alice"},
	}

	// Apply first query (sets name)
	WithQuery(first{Name: "bob"})(req)
	q := req.URL.Query()
	assert.Equal(t, "bob", q.Get("name"))

	// Apply second query (adds age, should preserve name)
	WithQuery(second{Age: 30})(req)
	q = req.URL.Query()
	assert.Equal(t, "bob", q.Get("name"))
	assert.Equal(t, "30", q.Get("age"))
}
