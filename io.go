// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

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

// InMemoryCacheStore implements CacheStore in memory.
type InMemoryCacheStore struct {
	mu    sync.RWMutex
	cache map[string]inMemoryCacheEntry
}

// NewInMemoryCacheStore creates a thread-safe in-memory CacheStore.
func NewInMemoryCacheStore() *InMemoryCacheStore {
	return &InMemoryCacheStore{
		cache: make(map[string]inMemoryCacheEntry),
	}
}

// Get retrieves cached response bytes from memory.
func (s *InMemoryCacheStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.cache[key]
	if !ok || time.Now().After(entry.ExpiresAt) {
		return nil, errors.New("aoni cache: miss")
	}

	return entry.Value, nil
}

// Set stores response bytes in memory with TTL.
func (s *InMemoryCacheStore) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cache[key] = inMemoryCacheEntry{
		Value:     val,
		ExpiresAt: time.Now().Add(ttl),
	}

	return nil
}

type inMemoryCacheEntry struct {
	Value     []byte
	ExpiresAt time.Time
}

type cachedResponse struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	BodyBase64 string              `json:"body_base64"`
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

	return &fallbackReplayableBody{
		ReadCloser: rc,
		buf:        buf,
		reader:     io.TeeReader(rc, buf),
	}
}

type progressReader struct {
	reader     io.Reader
	total      int64
	current    int64
	onProgress ProgressFunc
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	if n > 0 {
		cur := atomic.AddInt64(&pr.current, int64(n))
		pr.onProgress(cur, pr.total)
	}

	return n, err
}

func (pr *progressReader) Close() error {
	if closer, ok := pr.reader.(io.Closer); ok {
		return closer.Close()
	}

	return nil
}

type contextCancelingReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *contextCancelingReadCloser) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func (c *contextCancelingReadCloser) Unwrap() io.Closer { return c.ReadCloser }

type decompressReadCloser struct {
	io.Reader
	closer io.Closer
}

func (d *decompressReadCloser) Close() error {
	var firstErr error
	if c, ok := d.Reader.(io.Closer); ok {
		firstErr = c.Close()
	}

	if err := d.closer.Close(); err != nil && firstErr == nil {
		firstErr = err
	}

	return firstErr
}

func (d *decompressReadCloser) Unwrap() io.Closer { return d.closer }

type limitCheckingReadCloser struct {
	io.ReadCloser
	limit int64
	read  int64
}

func (l *limitCheckingReadCloser) Read(p []byte) (int, error) {
	n, err := l.ReadCloser.Read(p)

	l.read += int64(n)
	if l.read > l.limit {
		return n, ErrResponseTooLarge
	}

	return n, err
}

func (l *limitCheckingReadCloser) Unwrap() io.Closer { return l.ReadCloser }

type responseBodyReadCloser struct {
	io.ReadCloser
}

func newResponseBodyReadCloser(rc io.ReadCloser) io.ReadCloser {
	return &responseBodyReadCloser{ReadCloser: rc}
}

func (r *responseBodyReadCloser) Close() error {
	err := r.ReadCloser.Close()
	if rb, ok := unwrapBody(r.ReadCloser).(interface{ ReallyClose() }); ok {
		rb.ReallyClose()
	}

	return err
}

func (r *responseBodyReadCloser) Unwrap() io.Closer {
	return r.ReadCloser
}

type multiReadBody struct {
	data    []byte
	tmpFile *os.File
	reader  io.Reader
	mu      sync.Mutex
	closed  bool
}

func newMultiReadBody(rc io.ReadCloser, threshold int64, disableDisk bool) (io.ReadCloser, error) {
	var buf bytes.Buffer

	limitReader := io.LimitReader(rc, threshold+1)

	_, err := io.Copy(&buf, limitReader)
	if err != nil {
		_ = rc.Close()
		return nil, err
	}

	m := &multiReadBody{}

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

func (m *multiReadBody) Read(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reader.Read(p)
}

// Reset resets the read cursor so the body can be read again.
func (m *multiReadBody) Reset() {
	_ = m.Close()
}

// Close resets the read cursor so the body can be read again (multiRead semantics).
// It does NOT delete temporary files; call ReallyClose for that.
func (m *multiReadBody) Close() error {
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
func (m *multiReadBody) ReallyClose() {
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

type fallbackReplayableBody struct {
	io.ReadCloser
	buf    *bytes.Buffer
	reader io.Reader
}

func (f *fallbackReplayableBody) Read(p []byte) (int, error) {
	return f.reader.Read(p)
}

func (f *fallbackReplayableBody) Reset() {
	f.reader = io.MultiReader(f.buf, f.ReadCloser)
}

type jitterReader struct {
	io.ReadCloser
	delay time.Duration
	once  sync.Once
}

func (r *jitterReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		time.Sleep(r.delay)
	})

	return r.ReadCloser.Read(p)
}

// bufferedConn wraps a net.Conn with a bufio.Reader so that leftover bytes
// buffered during HTTP response parsing are returned before real network data.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) {
	if c.r.Buffered() > 0 {
		return c.r.Read(b)
	}

	return c.Conn.Read(b)
}
