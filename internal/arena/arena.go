// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package arena

import (
	"io"
	"sync"
	"unsafe"
)

const ArenaSize = 32 * 1024 // 32 KB

type Arena struct {
	buf    []byte
	offset int
}

var arenaPool = sync.Pool{
	New: func() any {
		return &Arena{
			buf: make([]byte, ArenaSize),
		}
	},
}

func Acquire() *Arena {
	return arenaPool.Get().(*Arena)
}

func Release(a *Arena) {
	a.offset = 0
	arenaPool.Put(a)
}

func (a *Arena) Reset() {
	a.offset = 0
}

func (a *Arena) AllocBytes(n int) []byte {
	if a.offset+n > len(a.buf) {
		return make([]byte, n)
	}

	start := a.offset
	a.offset += n

	return a.buf[start:a.offset]
}

func Alloc[T any](a *Arena) *T {
	var zero T

	size := unsafe.Sizeof(zero)
	a.offset = (a.offset + 7) &^ 7

	if a.offset+int(size) > len(a.buf) {
		return new(T)
	}

	ptr := unsafe.Pointer(&a.buf[a.offset])
	a.offset += int(size)

	return (*T)(ptr)
}

func (a *Arena) ReadAll(r io.Reader) ([]byte, error) {
	start := a.offset

	for {
		if a.offset == len(a.buf) {
			b, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}

			return append(a.buf[start:a.offset], b...), nil
		}

		n, err := r.Read(a.buf[a.offset:])
		a.offset += n

		if err != nil {
			if err == io.EOF {
				return a.buf[start:a.offset], nil
			}

			return nil, err
		}
	}
}
