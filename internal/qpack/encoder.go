// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package qpack

import "io"

// Encoder serializes HTTP/3 headers into QPACK header blocks (RFC 9204 §4.5).
type Encoder struct {
	w           io.Writer
	buf         []byte
	wrotePrefix bool
}

// NewEncoder initializes a new QPACK Encoder writing to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{
		w:   w,
		buf: make([]byte, 0, 512),
	}
}

// Reset resets the encoder state to write to w without allocations.
func (e *Encoder) Reset(w io.Writer) {
	e.w = w
	e.buf = e.buf[:0]
	e.wrotePrefix = false
}

// WriteField writes a single HeaderField to the output stream.
func (e *Encoder) WriteField(hf HeaderField) error {
	if !e.wrotePrefix {
		// RFC 9204 §4.5.1: Encoded Field Section Prefix
		// Required Insert Count = 0 (0x00)
		// Sign bit S = 0, Delta Base = 0 (0x00)
		e.buf = append(e.buf, 0x00, 0x00)
		e.wrotePrefix = true
	}

	exactIdx, nameIdx, hasExact, hasName := findStatic(hf.Name, hf.Value)
	switch {
	case hasExact:
		// RFC 9204 §4.5.2: Indexed Field Line (Static Table T=1)
		// 1 1 [6-bit Static Table Index]
		e.buf = append(e.buf, 0xc0)
		e.buf = appendInt(e.buf, 6, uint64(exactIdx))

	case hasName:
		// RFC 9204 §4.5.4: Literal Field Line With Name Reference (Static Table T=1, N=0)
		// 0 1 0 1 [4-bit Static Table Index]
		e.buf = append(e.buf, 0x50)
		e.buf = appendInt(e.buf, 4, uint64(nameIdx))
		e.buf = appendString(e.buf, hf.Value)

	default:
		// RFC 9204 §4.5.6: Literal Field Line With Literal Name (N=0)
		// 0 0 1 0 H [3-bit Name Length Prefix]
		hLen := huffmanLen(hf.Name)
		if hLen < len(hf.Name) {
			e.buf = append(e.buf, 0x28) // 0x20 | 0x08 (H=1)
			e.buf = appendInt(e.buf, 3, uint64(hLen))
			e.buf = appendHuffman(e.buf, hf.Name)
		} else {
			e.buf = append(e.buf, 0x20) // 0x20 | 0x00 (H=0)
			e.buf = appendInt(e.buf, 3, uint64(len(hf.Name)))
			e.buf = append(e.buf, hf.Name...)
		}

		e.buf = appendString(e.buf, hf.Value)
	}

	if e.w != nil && len(e.buf) > 0 {
		_, err := e.w.Write(e.buf)
		e.buf = e.buf[:0]

		return err
	}

	return nil
}
