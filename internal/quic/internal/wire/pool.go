// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package wire

import (
	"sync"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
)

var pool sync.Pool

func init() {
	pool.New = func() any {
		return &StreamFrame{
			Data:     make([]byte, 0, protocol.MaxPacketBufferSize),
			fromPool: true,
		}
	}
}

func GetStreamFrame() *StreamFrame {
	f := pool.Get().(*StreamFrame)
	return f
}

func putStreamFrame(f *StreamFrame) {
	if !f.fromPool {
		return
	}

	if protocol.ByteCount(cap(f.Data)) != protocol.MaxPacketBufferSize {
		panic("wire.PutStreamFrame called with packet of wrong size!")
	}

	pool.Put(f)
}
