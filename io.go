// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
)

type progressReader struct {
	reader     io.Reader
	total      int64
	current    int64
	onProgress ProgressFunc
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	if n > 0 {
		pr.current += int64(n)
		pr.onProgress(pr.current, pr.total)
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

type finalizerReadCloser struct {
	io.ReadCloser
	closed atomic.Bool
}

func newFinalizerReadCloser(rc io.ReadCloser) io.ReadCloser {
	f := &finalizerReadCloser{ReadCloser: rc}
	runtime.SetFinalizer(f, func(fr *finalizerReadCloser) {
		if !fr.closed.Load() {
			fmt.Fprintln(
				os.Stderr,
				"WARNING: aoni: response body was not closed, closing automatically via GC finalizer",
			)

			_ = fr.ReadCloser.Close()
			// Also ensure any temp-files in the chain are cleaned up.
			if rb, ok := unwrapBody(fr.ReadCloser).(interface{ ReallyClose() }); ok {
				rb.ReallyClose()
			}
		}
	})

	return f
}

func (f *finalizerReadCloser) Close() error {
	if f.closed.CompareAndSwap(false, true) {
		runtime.SetFinalizer(f, nil)
		return f.ReadCloser.Close()
	}

	return nil
}

func (f *finalizerReadCloser) Unwrap() io.Closer { return f.ReadCloser }

// bomStrippingReadCloser wraps a BOM-stripping reader around an existing ReadCloser.
// It implements Unwrap so that closeResponse can traverse the wrapper chain.
type bomStrippingReadCloser struct {
	io.Reader
	io.Closer
}

func (b *bomStrippingReadCloser) Unwrap() io.Closer { return b.Closer }

type bomStrippingReader struct {
	reader io.Reader
	header []byte
	offset int
}

func newBOMStrippingReader(r io.Reader) io.Reader {
	return &bomStrippingReader{reader: r}
}

func (b *bomStrippingReader) Read(p []byte) (int, error) {
	if b.header == nil {
		b.header = make([]byte, 3)

		n, err := io.ReadAtLeast(b.reader, b.header, 3)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return 0, err
		}

		b.header = b.header[:n]

		// Detect UTF-8 BOM
		switch {
		case len(b.header) >= 3 && b.header[0] == 0xEF && b.header[1] == 0xBB && b.header[2] == 0xBF:
			b.offset = 3
		case len(b.header) >= 2 && b.header[0] == 0xFE && b.header[1] == 0xFF:
			b.offset = 2
		case len(b.header) >= 2 && b.header[0] == 0xFF && b.header[1] == 0xFE:
			b.offset = 2
		default:
			b.offset = 0
		}
	}

	if b.offset < len(b.header) {
		n := copy(p, b.header[b.offset:])
		b.offset += n
		return n, nil
	}

	return b.reader.Read(p)
}

type multiReadBody struct {
	data    []byte
	tmpFile *os.File
	reader  io.Reader
	mu      sync.Mutex
	closed  bool
}

func newMultiReadBody(rc io.ReadCloser, threshold int64) (io.ReadCloser, error) {
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
// any temporary file. It is called by closeResponse (and the GC finalizer)
// after the body is no longer needed.
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
