// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

// Package io provides low-level streaming I/O wrappers and response body decorators.
package io

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/miyako/generic"
)

// ErrBufferLimitExceeded indicates the replayable buffer exceeded its memory threshold,
// and disk caching was disabled.
var ErrBufferLimitExceeded = errors.New("aoni: replayable buffer threshold exceeded")

// ErrResponseTooLarge indicates the response exceeded the size limit.
var ErrResponseTooLarge = errors.New("aoni: response size limit exceeded")

// ProgressFunc is called periodically during response body reads.
// current is the bytes read so far; total is the Content-Length
// value or -1 if unknown.
type ProgressFunc func(current, total int64)

// UnwrapBody unwraps the body of a response, following any decorator chain.
func UnwrapBody(c io.Closer) io.Closer {
	for {
		u, ok := c.(interface{ Unwrap() io.Closer })
		if !ok {
			break
		}

		c = u.Unwrap()
	}

	return c
}

// UnwrapTo traverses the decorator chain c and returns the first layer
// that implements the generic type T, as well as true.
// If no matching layer is found, returns the null value of T and false.
func UnwrapTo[T any](c io.Closer) (T, bool) {
	curr := c
	for {
		if val, ok := curr.(T); ok {
			return val, true
		}

		u, ok := curr.(interface{ Unwrap() io.Closer })
		if !ok {
			break
		}

		curr = u.Unwrap()
	}

	return generic.Zero[T](), false
}

// ReadAllString reads the entire content of a ReplayableBody as a string,
// and automatically resets the body so it remains completely reusable.
// This is a high-level helper for quick logging and assertions.
func ReadAllString(rb ReplayableBody) (string, error) {
	if rb == nil {
		return "", nil
	}

	defer rb.Reset()

	b, err := io.ReadAll(rb)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// ReadAllBytes reads the entire content of a ReplayableBody as a byte slice,
// and automatically resets the body so it remains completely reusable.
func ReadAllBytes(rb ReplayableBody) ([]byte, error) {
	if rb == nil {
		return nil, nil
	}

	defer rb.Reset()

	b, err := io.ReadAll(rb)
	if err != nil {
		return nil, err
	}

	return b, nil
}

// ExplicitBufferedBody wraps a response body where a prefix has been read and cached in memory.
// It allows reading the prefix followed by the remaining stream.
type ExplicitBufferedBody struct {
	Prefix []byte
	Stream io.ReadCloser

	reader io.Reader
}

// Read reads from the prefix followed by the underlying response stream.
func (e *ExplicitBufferedBody) Read(p []byte) (n int, err error) {
	if e.reader == nil {
		e.reader = io.MultiReader(bytes.NewReader(e.Prefix), e.Stream)
	}

	return e.reader.Read(p)
}

// Close closes the underlying response stream.
func (e *ExplicitBufferedBody) Close() error {
	return e.Stream.Close()
}

// Rewind resets the internal reader so that the body can be read from the beginning again.
func (e *ExplicitBufferedBody) Rewind() {
	e.reader = io.MultiReader(bytes.NewReader(e.Prefix), e.Stream)
}

// BufferedPrefix returns the pre-buffered byte prefix of the response body.
func (e *ExplicitBufferedBody) BufferedPrefix() []byte {
	return e.Prefix
}

// ReplayableBody represents a response stream that can be reset
// to the beginning for re-reading (for example, after previewing or logging).
type ReplayableBody interface {
	io.ReadCloser
	Reset()
}

// AsReplayable turns any [io.ReadCloser] into a [ReplayableBody].
// If there's already a high-performance buffer ([multiReadBody]) under the hood, it will return it.
// If not, it will transparently create a lightweight in-memory buffer for repeated reading.
func AsReplayable(rc io.ReadCloser) ReplayableBody {
	if rc == nil {
		return nil
	}

	curr := io.Closer(rc)
	for {
		if rb, ok := curr.(ReplayableBody); ok {
			return rb
		}

		u, ok := curr.(interface{ Unwrap() io.Closer })
		if !ok {
			break
		}

		curr = u.Unwrap()
	}

	buf := &bytes.Buffer{}

	return &FallbackReplayableBody{
		ReadCloser: rc,
		buf:        buf,
		reader:     io.TeeReader(rc, buf),
	}
}

// ProgressReader wraps an [io.Reader] and calls a progress function as it reads.
type ProgressReader struct {
	io.Reader
	Total      int64
	OnProgress ProgressFunc

	current int64
}

func (pr *ProgressReader) Read(p []byte) (n int, err error) {
	n, err = pr.Reader.Read(p)
	if n > 0 {
		cur := atomic.AddInt64(&pr.current, int64(n))
		pr.OnProgress(cur, pr.Total)
	}

	return n, err
}

// Close closes the underlying reader and returns any error.
func (pr *ProgressReader) Close() error {
	if closer, ok := pr.Reader.(io.Closer); ok {
		return closer.Close()
	}

	return nil
}

// ContextCancelingReadCloser wraps an [io.ReadCloser] and cancels a context when closed.
type ContextCancelingReadCloser struct {
	io.ReadCloser
	Cancel context.CancelFunc
}

// Close closes the underlying reader and cancels the context.
func (c *ContextCancelingReadCloser) Close() error {
	err := c.ReadCloser.Close()
	c.Cancel()
	return err
}

func (c *ContextCancelingReadCloser) Unwrap() io.Closer { return c.ReadCloser }

// DecompressReadCloser wraps an [io.Reader] and an [io.Closer] for decompressing data.
type DecompressReadCloser struct {
	io.Reader
	Closer io.Closer
}

// Close closes the underlying reader and returns any error.
func (d *DecompressReadCloser) Close() error {
	var firstErr error
	if c, ok := d.Reader.(io.Closer); ok {
		firstErr = c.Close()
	}

	if err := d.Closer.Close(); err != nil && firstErr == nil {
		firstErr = err
	}

	return firstErr
}

func (d *DecompressReadCloser) Unwrap() io.Closer { return d.Closer }

// LimitCheckingReadCloser wraps an [io.ReadCloser] and checks the read limit.
type LimitCheckingReadCloser struct {
	io.ReadCloser
	Limit int64
	read  int64
}

func (l *LimitCheckingReadCloser) Read(p []byte) (int, error) {
	n, err := l.ReadCloser.Read(p)

	l.read += int64(n)
	if l.read > l.Limit {
		return n, ErrResponseTooLarge
	}

	return n, err
}

func (l *LimitCheckingReadCloser) Unwrap() io.Closer { return l.ReadCloser }

// ResponseBodyReadCloser wraps an [io.ReadCloser] for reading the response body.
type ResponseBodyReadCloser struct {
	io.ReadCloser
}

// Close closes the underlying ReadCloser and performs any necessary cleanup.
func (r *ResponseBodyReadCloser) Close() error {
	err := r.ReadCloser.Close()
	if rb, ok := UnwrapBody(r.ReadCloser).(interface{ ReallyClose() }); ok {
		rb.ReallyClose()
	}

	return err
}

func (r *ResponseBodyReadCloser) Unwrap() io.Closer {
	return r.ReadCloser
}

// MultiReadBody wraps an [io.ReadCloser] to read data into memory or a temporary file.
type MultiReadBody struct {
	data    []byte
	tmpFile *os.File
	reader  io.Reader
	mu      sync.Mutex
	closed  bool
}

// NewMultiReadBody returns a new [MultiReadBody] that wraps the given [io.ReadCloser].
func NewMultiReadBody(rc io.ReadCloser, threshold int64, disableDisk bool) (io.ReadCloser, error) {
	var buf bytes.Buffer

	limitReader := io.LimitReader(rc, threshold+1)

	_, err := io.Copy(&buf, limitReader)
	if err != nil {
		_ = rc.Close()
		return nil, err
	}

	m := &MultiReadBody{}

	if int64(buf.Len()) <= threshold {
		_ = rc.Close()
		m.data = buf.Bytes()
		m.reader = bytes.NewReader(m.data)
	} else {
		if disableDisk {
			_ = rc.Close()
			return nil, ErrBufferLimitExceeded
		}

		tmpFile, err := os.CreateTemp("", "aoni-multiread-*")
		if err != nil {
			_ = rc.Close()
			return nil, err
		}

		_, err = tmpFile.Write(buf.Bytes())
		if err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpFile.Name())
			_ = rc.Close()

			return nil, err
		}

		_, err = io.Copy(tmpFile, rc)

		_ = rc.Close()
		if err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpFile.Name())
			return nil, err
		}

		_, _ = tmpFile.Seek(0, io.SeekStart)
		m.tmpFile = tmpFile
		m.reader = tmpFile
	}

	return m, nil
}

func (m *MultiReadBody) Read(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reader.Read(p)
}

// Reset resets the read cursor so the body can be read again.
func (m *MultiReadBody) Reset() {
	_ = m.Close()
}

// Close resets the read cursor so the body can be read again (multiRead semantics).
// It does NOT delete temporary files; call ReallyClose for that.
func (m *MultiReadBody) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.tmpFile != nil {
		_, _ = m.tmpFile.Seek(0, io.SeekStart)
		m.reader = m.tmpFile
	} else {
		m.reader = bytes.NewReader(m.data)
	}

	return nil
}

// ReallyClose performs the actual resource teardown: it closes and removes
// any underlying temporary file from disk.
//
// # Preconditions
//
// Once ReallyClose is called, the multiReadBody becomes completely unusable
// and cannot be reset or read again. This method must only be called when
// the response is no longer needed (e.g., inside closeResponse).
func (m *MultiReadBody) ReallyClose() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return
	}

	m.closed = true

	if m.tmpFile != nil {
		_ = m.tmpFile.Close()
		_ = os.Remove(m.tmpFile.Name())
		m.tmpFile = nil
	}
}

// FallbackReplayableBody wraps an [io.ReadCloser] and a buffer to allow
// replaying buffered data before reading from the underlying ReadCloser.
type FallbackReplayableBody struct {
	io.ReadCloser
	buf    *bytes.Buffer
	reader io.Reader
}

func (f *FallbackReplayableBody) Read(p []byte) (int, error) {
	return f.reader.Read(p)
}

// Reset reinitializes the reader to replay the buffer before the underlying ReadCloser.
func (f *FallbackReplayableBody) Reset() {
	f.reader = io.MultiReader(f.buf, f.ReadCloser)
}

// JitterReader wraps an [io.ReadCloser] and adds a fixed delay before each read.
type JitterReader struct {
	io.ReadCloser
	Delay time.Duration
	once  sync.Once
}

func (r *JitterReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		time.Sleep(r.Delay)
	})

	return r.ReadCloser.Read(p)
}

// BufferedConn wraps a net.Conn with a bufio.Reader so that leftover bytes
// buffered during HTTP response parsing are returned before real network data.
type BufferedConn struct {
	net.Conn
	R *bufio.Reader
}

func (c *BufferedConn) Read(b []byte) (int, error) {
	if c.R.Buffered() > 0 {
		return c.R.Read(b)
	}

	return c.Conn.Read(b)
}
