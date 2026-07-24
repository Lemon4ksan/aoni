// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package io provides high-performance streaming I/O wrappers and response body decorators.
package io

import (
	"bufio"
	"bytes"
	"compress/gzip"
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

var (
	// ErrBufferLimitExceeded indicates that replayable payload size exceeded RAM bounds with disk backing disabled.
	ErrBufferLimitExceeded = errors.New("aoni: replayable buffer threshold exceeded")

	// ErrResponseTooLarge indicates that response payload length exceeded configured maximum bounds.
	ErrResponseTooLarge = errors.New("aoni: response size limit exceeded")
)

// ProgressFunc reports periodic stream transfer progress (current bytes and total Content-Length).
type ProgressFunc func(current, total int64)

const maxPoolBufferSize = 64 * 1024

var copyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

// CopyZeroAlloc streams data from r to w using kernel zero-copy paths or pooled 32KB buffers.
func CopyZeroAlloc(w io.Writer, r io.Reader) (int64, error) {
	if r == nil || w == nil {
		return 0, nil
	}

	if rf, ok := w.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}

	if wt, ok := r.(io.WriterTo); ok {
		return wt.WriteTo(w)
	}

	bufPtr := copyBufPool.Get().(*[]byte)
	defer func() {
		if cap(*bufPtr) <= maxPoolBufferSize {
			copyBufPool.Put(bufPtr)
		}
	}()

	return io.CopyBuffer(w, r, *bufPtr)
}

// UnwrapBody unwraps nested response body decorators down to the innermost [io.Closer].
func UnwrapBody(c io.Closer) io.Closer {
	curr := c
	for {
		u, ok := curr.(interface{ Unwrap() io.Closer })
		if !ok {
			break
		}

		curr = u.Unwrap()
	}

	return curr
}

// UnwrapTo traverses decorator chain c and returns the first layer satisfying target type T.
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

// ReadAllString reads stream content into a string and resets the reader position.
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

// ReadAllBytes reads stream content into a byte slice and resets the reader position.
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

// ExplicitBufferedBody wraps a response stream carrying a pre-buffered byte prefix buffer.
type ExplicitBufferedBody struct {
	Stream io.ReadCloser
	reader io.Reader
	Prefix []byte
}

func (e *ExplicitBufferedBody) Read(p []byte) (int, error) {
	if e.reader == nil {
		e.reader = io.MultiReader(bytes.NewReader(e.Prefix), e.Stream)
	}

	return e.reader.Read(p)
}

// Close closes the underlying stream.
func (e *ExplicitBufferedBody) Close() error {
	return e.Stream.Close()
}

// Rewind resets the reader to the beginning of the stream, including the buffered prefix.
func (e *ExplicitBufferedBody) Rewind() {
	e.reader = io.MultiReader(bytes.NewReader(e.Prefix), e.Stream)
}

// BufferedPrefix returns the pre-buffered prefix bytes.
func (e *ExplicitBufferedBody) BufferedPrefix() []byte {
	return e.Prefix
}

// ReplayableBody defines a stream that can be rewinded back to position 0 for repeated reads.
type ReplayableBody interface {
	io.ReadCloser
	Reset()
}

// AsReplayable wraps rc into a [ReplayableBody] using active buffers or tee-buffered fallback.
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

// ProgressReader invokes [ProgressFunc] callbacks as data is read from the stream.
type ProgressReader struct {
	io.Reader
	OnProgress ProgressFunc
	Total      int64
	current    int64
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	if n > 0 {
		cur := atomic.AddInt64(&pr.current, int64(n))
		pr.OnProgress(cur, pr.Total)
	}

	return n, err
}

// Close closes the underlying reader if it implements [io.Closer].
func (pr *ProgressReader) Close() error {
	if closer, ok := pr.Reader.(io.Closer); ok {
		return closer.Close()
	}

	return nil
}

// ContextCancelingReadCloser triggers context cancellation when closed.
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

// DecompressReadCloser combines decompression streams with underlying resource closers.
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

var gzipReaderPool = sync.Pool{
	New: func() any {
		return new(gzip.Reader)
	},
}

// NewPooledGzipReader retrieves a reset [*gzip.Reader] from [sync.Pool] without heap allocations.
func NewPooledGzipReader(r io.Reader) (io.ReadCloser, error) {
	gr := gzipReaderPool.Get().(*gzip.Reader)
	if err := gr.Reset(r); err != nil {
		gzipReaderPool.Put(gr)
		return nil, err
	}

	return &pooledGzipReadCloser{gr: gr, reader: r}, nil
}

type pooledGzipReadCloser struct {
	gr     *gzip.Reader
	reader io.Reader
}

func (p *pooledGzipReadCloser) Read(b []byte) (int, error) {
	return p.gr.Read(b)
}

func (p *pooledGzipReadCloser) Close() error {
	_ = p.gr.Close()
	gzipReaderPool.Put(p.gr)

	if c, ok := p.reader.(io.Closer); ok {
		return c.Close()
	}

	return nil
}

func (p *pooledGzipReadCloser) Unwrap() io.Closer {
	if c, ok := p.reader.(io.Closer); ok {
		return c
	}

	return nil
}

// LimitCheckingReadCloser caps byte consumption and returns [ErrResponseTooLarge] on overflow.
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

// ResponseBodyReadCloser manages response stream teardown.
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

func (r *ResponseBodyReadCloser) Unwrap() io.Closer { return r.ReadCloser }

// MultiReadBody buffers payload streams in RAM or temp files for repeatable reads.
type MultiReadBody struct {
	tmpFile *os.File
	reader  io.Reader
	data    []byte
	mu      sync.Mutex
	closed  bool
}

// NewMultiReadBody creates a [MultiReadBody] wrapping rc using RAM or disk buffers.
func NewMultiReadBody(rc io.ReadCloser, threshold int64, disableDisk bool) (io.ReadCloser, error) {
	if rc == nil {
		return nil, nil
	}

	var buf bytes.Buffer

	limitReader := io.LimitReader(rc, threshold+1)

	if _, err := CopyZeroAlloc(&buf, limitReader); err != nil {
		_ = rc.Close()
		return nil, err
	}

	if int64(buf.Len()) <= threshold {
		_ = rc.Close()
		data := buf.Bytes()

		return &MultiReadBody{
			data:   data,
			reader: bytes.NewReader(data),
		}, nil
	}

	if disableDisk {
		_ = rc.Close()
		return nil, ErrBufferLimitExceeded
	}

	return createDiskBackedMultiReadBody(rc, buf.Bytes())
}

func createDiskBackedMultiReadBody(rc io.ReadCloser, initialBytes []byte) (io.ReadCloser, error) {
	defer rc.Close()

	tmpFile, err := os.CreateTemp("", "aoni-multiread-*")
	if err != nil {
		return nil, err
	}

	if err := writeInitialAndStreamData(tmpFile, rc, initialBytes); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())

		return nil, err
	}

	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())

		return nil, err
	}

	return &MultiReadBody{
		tmpFile: tmpFile,
		reader:  tmpFile,
	}, nil
}

func writeInitialAndStreamData(tmpFile *os.File, rc io.Reader, initialBytes []byte) error {
	if _, err := tmpFile.Write(initialBytes); err != nil {
		return err
	}

	_, err := CopyZeroAlloc(tmpFile, rc)

	return err
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

// Close resets the read cursor so the body can be read again.
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

// ReallyClose performs actual resource teardown: closes and removes temporary files.
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

// FallbackReplayableBody provides tee-buffered replayable stream reads.
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

// JitterReader applies artificial delay prior to initial stream reads.
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

// BufferedConn wraps a [net.Conn] with a buffered reader.
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

// BufioReadCloser wraps a [*bufio.Reader] and an [io.Closer].
type BufioReadCloser struct {
	*bufio.Reader
	Closer io.Closer
}

// Close closes the underlying Closer, if any.
func (b *BufioReadCloser) Close() error {
	if b.Closer != nil {
		return b.Closer.Close()
	}

	return nil
}

// BufioReader returns the underlying bufio.Reader.
func (b *BufioReadCloser) BufioReader() *bufio.Reader {
	return b.Reader
}
