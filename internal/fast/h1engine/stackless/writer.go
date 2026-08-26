// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stackless

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

// Writer is an interface stackless writer must conform to.
//
// The interface contains common subset for Writers from compress/* packages.
type Writer interface {
	Write(p []byte) (int, error)
	Flush() error
	Close() error
	Reset(w io.Writer)
}

// NewWriterFunc must return new writer that will be wrapped into
// stackless writer.
type NewWriterFunc func(w io.Writer) Writer

// NewWriter creates a stackless writer around a writer returned
// from newWriter.
//
// The returned writer writes data to dstW.
//
// Writers that use a lot of stack space may be wrapped into stackless writer,
// thus saving stack space for high number of concurrently running goroutines.
func NewWriter(dstW io.Writer, newWriter NewWriterFunc) Writer {
	w := &writer{
		dstW: dstW,
	}
	w.zw = newWriter(&w.xw)
	return w
}

type writer struct {
	dstW io.Writer
	zw   Writer

	err error
	xw  xWriter

	p []byte
	n int

	op op
}

type op int

const (
	opWrite op = iota
	opFlush
	opClose
	opReset
)

func (w *writer) Write(p []byte) (int, error) {
	w.p = p
	err := w.do(opWrite)
	w.p = nil
	return w.n, err
}

func (w *writer) WriteString(s string) (int, error) {
	w.p = s2b(s)
	err := w.do(opWrite)
	w.p = nil
	return w.n, err
}

func (w *writer) Flush() error {
	return w.do(opFlush)
}

func (w *writer) Close() error {
	return w.do(opClose)
}

func (w *writer) Reset(dstW io.Writer) {
	w.xw.Reset()
	w.do(opReset) //nolint:errcheck
	w.dstW = dstW
}

func (w *writer) do(op op) error {
	w.op = op
	if !stacklessWriterFunc(w) {
		return errHighLoad
	}
	err := w.err
	if err != nil {
		return err
	}
	if w.xw.bb != nil && len(w.xw.bb.b) > 0 {
		_, err = w.dstW.Write(w.xw.bb.b)
	}
	w.xw.Reset()

	return err
}

var errHighLoad = errors.New("cannot compress data due to high load")

var (
	stacklessWriterFuncOnce sync.Once
	stacklessWriterFuncFunc func(ctx any) bool
)

func stacklessWriterFunc(ctx any) bool {
	stacklessWriterFuncOnce.Do(func() {
		stacklessWriterFuncFunc = NewFunc(writerFunc)
	})
	return stacklessWriterFuncFunc(ctx)
}

func writerFunc(ctx any) {
	w := ctx.(*writer) //nolint:forcetypeassert
	switch w.op {
	case opWrite:
		w.n, w.err = w.zw.Write(w.p)
	case opFlush:
		w.err = w.zw.Flush()
	case opClose:
		w.err = w.zw.Close()
	case opReset:
		w.zw.Reset(&w.xw)
		w.err = nil
	default:
		panic(fmt.Sprintf("BUG: unexpected op: %d", w.op))
	}
}

type byteBuffer struct {
	b []byte
}

func (b *byteBuffer) Write(p []byte) (int, error) {
	b.b = append(b.b, p...)
	return len(p), nil
}

func (b *byteBuffer) Reset() {
	b.b = b.b[:0]
}

type xWriter struct {
	bb *byteBuffer
}

func (w *xWriter) Write(p []byte) (int, error) {
	if w.bb == nil {
		w.bb = acquireByteBuffer()
	}
	return w.bb.Write(p)
}

func (w *xWriter) Reset() {
	if w.bb != nil {
		releaseByteBuffer(w.bb)
		w.bb = nil
	}
}

var bufferPool = sync.Pool{
	New: func() any {
		return &byteBuffer{}
	},
}

func acquireByteBuffer() *byteBuffer {
	return bufferPool.Get().(*byteBuffer) //nolint:forcetypeassert
}

func releaseByteBuffer(b *byteBuffer) {
	b.Reset()
	bufferPool.Put(b)
}
