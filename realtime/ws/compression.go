// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ws

import (
	"bytes"
	"fmt"
	"io"

	"github.com/lemon4ksan/foundation/borrow"
	"github.com/lemon4ksan/foundation/silicon/pool"

	"github.com/lemon4ksan/aoni/internal/compress/flate"
)

var (
	wsTail = [9]byte{0x00, 0x00, 0xff, 0xff, 0x01, 0x00, 0x00, 0xff, 0xff}

	flateReaderStorage = pool.NewPerPStorage(func() flate.Resetter {
		return flate.NewReader(nil).(flate.Resetter)
	})

	flateWriterStorage = pool.NewPerPStorage(func() *flate.Writer {
		w, _ := flate.NewWriter(nil, flate.DefaultCompression)
		return w
	})

	compressBufferStorage = pool.NewPerPStorage(func() *bytes.Buffer {
		return bytes.NewBuffer(make([]byte, 0, 4096))
	})

	decompressBufferStorage = pool.NewPerPStorage(func() *[]byte {
		buf := make([]byte, 0, 4096)
		return &buf
	})
)

type twoSliceReader struct {
	s1  []byte
	s2  []byte
	off int
}

func (r *twoSliceReader) Reset(s1, s2 []byte) {
	r.s1 = s1
	r.s2 = s2
	r.off = 0
}

func (r *twoSliceReader) Read(p []byte) (int, error) {
	if r.off < len(r.s1) {
		n := copy(p, r.s1[r.off:])
		r.off += n
		return n, nil
	}

	off2 := r.off - len(r.s1)
	if off2 < len(r.s2) {
		n := copy(p, r.s2[off2:])
		r.off += n
		return n, nil
	}

	return 0, io.EOF
}

// compressNoContextTakeover compresses payload bytes per RFC 7692 Section 7.2.1,
// stripping trailing 0x00 0x00 0xFF 0xFF bytes after flushing.
func compressNoContextTakeover(src []byte) ([]byte, error) {
	buf := compressBufferStorage.Get()
	defer compressBufferStorage.Put(buf)

	buf.Reset()

	fw := flateWriterStorage.Get()
	defer flateWriterStorage.Put(fw)

	fw.Reset(buf)

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
	var r twoSliceReader
	r.Reset(src, wsTail[:])

	fr := flateReaderStorage.Get()
	defer flateReaderStorage.Put(fr)

	if err := fr.Reset(&r, nil); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFlateDecompressFailed, err)
	}

	readCloser, ok := fr.(io.ReadCloser)
	if !ok {
		return nil, ErrFlateDecompressFailed
	}

	outBuf := decompressBufferStorage.Get()
	defer decompressBufferStorage.Put(outBuf)

	*outBuf = (*outBuf)[:0]

	var chunk [4096]byte
	for {
		n, err := readCloser.Read(chunk[:])
		if n > 0 {
			*outBuf = append(*outBuf, chunk[:n]...)
		}

		if err != nil {
			if err == io.EOF {
				break
			}

			return nil, fmt.Errorf("%w: %w", ErrFlateDecompressFailed, err)
		}
	}

	res := make([]byte, len(*outBuf))
	copy(res, *outBuf)

	return res, nil
}

// decompressNoContextTakeoverScoped decompresses payload bytes into arena memory bound to scope.
func decompressNoContextTakeoverScoped(src []byte, scope *borrow.Scope) ([]byte, error) {
	var r twoSliceReader
	r.Reset(src, wsTail[:])

	fr := flateReaderStorage.Get()
	defer flateReaderStorage.Put(fr)

	if err := fr.Reset(&r, nil); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFlateDecompressFailed, err)
	}

	readCloser, ok := fr.(io.ReadCloser)
	if !ok {
		return nil, ErrFlateDecompressFailed
	}

	outBuf := decompressBufferStorage.Get()
	defer decompressBufferStorage.Put(outBuf)

	*outBuf = (*outBuf)[:0]

	var chunk [4096]byte
	for {
		n, err := readCloser.Read(chunk[:])
		if n > 0 {
			*outBuf = append(*outBuf, chunk[:n]...)
		}

		if err != nil {
			if err == io.EOF {
				break
			}

			return nil, fmt.Errorf("%w: %w", ErrFlateDecompressFailed, err)
		}
	}

	if scope != nil {
		borrowed := scope.AllocBytes(len(*outBuf))
		copy(borrowed.AsSlice(), *outBuf)

		return borrowed.AsSlice(), nil
	}

	res := make([]byte, len(*outBuf))
	copy(res, *outBuf)

	return res, nil
}
