// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"bytes"
	"sync"
)

// BufferPool manages reusable byte buffers to achieve zero-allocation operation during response processing.
type BufferPool struct {
	pool sync.Pool
}

// GlobalBufferPool is the singleton zero-allocation buffer pool for engine routines.
var GlobalBufferPool = NewBufferPool(32 * 1024)

// NewBufferPool instantiates a [BufferPool] with initial capacity.
func NewBufferPool(size int) *BufferPool {
	if size <= 0 {
		size = 32 * 1024
	}

	return &BufferPool{
		pool: sync.Pool{
			New: func() any {
				return bytes.NewBuffer(make([]byte, 0, size))
			},
		},
	}
}

// Get borrows a [*bytes.Buffer] from the pool.
func (p *BufferPool) Get() *bytes.Buffer {
	buf := p.pool.Get().(*bytes.Buffer)
	buf.Reset()

	return buf
}

// Put returns a [*bytes.Buffer] to the pool.
func (p *BufferPool) Put(buf *bytes.Buffer) {
	if buf == nil {
		return
	}

	if buf.Cap() > 512*1024 {
		return
	}

	buf.Reset()
	p.pool.Put(buf)
}
