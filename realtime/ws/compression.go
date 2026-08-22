// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ws

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni/internal/compress/flate"
)

var (
	flateReaderPool = sync.Pool{
		New: func() any {
			return flate.NewReader(nil)
		},
	}

	flateWriterPool = generic.NewPool(func() *flate.Writer {
		w, _ := flate.NewWriter(nil, flate.DefaultCompression)
		return w
	})

	compressBufferPool = generic.NewPool(func() *bytes.Buffer {
		return bytes.NewBuffer(make([]byte, 0, 4096))
	})
)

// compressNoContextTakeover compresses payload bytes per RFC 7692 Section 7.2.1,
// stripping trailing 0x00 0x00 0xFF 0xFF bytes after flushing.
func compressNoContextTakeover(src []byte) ([]byte, error) {
	buf := compressBufferPool.Get()
	if buf == nil {
		buf = bytes.NewBuffer(make([]byte, 0, max(len(src), 1024)))
	} else {
		buf.Reset()
	}

	defer func() {
		buf.Reset()
		compressBufferPool.Put(buf)
	}()

	fw := flateWriterPool.Get()
	if fw == nil {
		var err error

		fw, err = flate.NewWriter(buf, flate.DefaultCompression)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFlateCompressFailed, err)
		}
	} else {
		fw.Reset(buf)
	}

	defer flateWriterPool.Put(fw)

	if _, err := fw.Write(src); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFlateCompressFailed, err)
	}

	if err := fw.Flush(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFlateCompressFailed, err)
	}

	raw := buf.Bytes()
	if len(raw) >= 4 {
		raw = raw[:len(raw)-4]
	}

	out := make([]byte, len(raw))
	copy(out, raw)

	return out, nil
}

// decompressNoContextTakeover decompresses payload bytes per RFC 7692 Section 7.2.2,
// appending 0x00 0x00 0xFF 0xFF 0x01 0x00 0x00 0xFF 0xFF sync flush tail before decoding.
func decompressNoContextTakeover(src []byte) ([]byte, error) {
	tail := []byte{0x00, 0x00, 0xff, 0xff, 0x01, 0x00, 0x00, 0xff, 0xff}
	r := io.MultiReader(bytes.NewReader(src), bytes.NewReader(tail))

	fr, ok := flateReaderPool.Get().(io.ReadCloser)
	if !ok || fr == nil {
		fr = flate.NewReader(r)
	} else if resetter, ok := fr.(flate.Resetter); ok {
		_ = resetter.Reset(r, nil)
	} else {
		fr = flate.NewReader(r)
	}

	defer flateReaderPool.Put(fr)

	out, err := io.ReadAll(fr)
	_ = fr.Close()

	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFlateDecompressFailed, err)
	}

	return out, nil
}
