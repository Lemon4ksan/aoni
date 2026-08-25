// Copyright (c) 2016 the quic-go authors. All rights reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quic

import (
	"github.com/lemon4ksan/foundation/silicon/pool"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
)

type packetBuffer struct {
	Data []byte

	// refCount counts how many packets Data is used in.
	// It doesn't support concurrent use.
	// It is > 1 when used for coalesced packet.
	refCount int
}

// Split increases the refCount.
// It must be called when a packet buffer is used for more than one packet,
// e.g. when splitting coalesced packets.
func (b *packetBuffer) Split() {
	b.refCount++
}

// Decrement decrements the reference counter.
// It doesn't put the buffer back into the pool.
func (b *packetBuffer) Decrement() {
	b.refCount--
	if b.refCount < 0 {
		panic("negative packetBuffer refCount")
	}
}

// MaybeRelease puts the packet buffer back into the pool,
// if the reference counter already reached 0.
func (b *packetBuffer) MaybeRelease() {
	// only put the packetBuffer back if it's not used any more
	if b.refCount == 0 {
		b.putBack()
	}
}

// Release puts back the packet buffer into the pool.
// It should be called when processing is definitely finished.
func (b *packetBuffer) Release() {
	b.Decrement()

	if b.refCount != 0 {
		panic("packetBuffer refCount not zero")
	}

	b.putBack()
}

// Len returns the length of Data
func (b *packetBuffer) Len() protocol.ByteCount { return protocol.ByteCount(len(b.Data)) }
func (b *packetBuffer) Cap() protocol.ByteCount { return protocol.ByteCount(cap(b.Data)) }

func (b *packetBuffer) putBack() {
	if cap(b.Data) == protocol.MaxPacketBufferSize {
		packetBufferStorage.Put(b)
		return
	}

	if cap(b.Data) == protocol.MaxLargePacketBufferSize {
		largePacketBufferStorage.Put(b)
		return
	}

	panic("putPacketBuffer called with packet of wrong size!")
}

var (
	packetBufferStorage = pool.NewPerPStorage(func() *packetBuffer {
		return &packetBuffer{Data: make([]byte, 0, protocol.MaxPacketBufferSize)}
	})
	largePacketBufferStorage = pool.NewPerPStorage(func() *packetBuffer {
		return &packetBuffer{Data: make([]byte, 0, protocol.MaxLargePacketBufferSize)}
	})
)

func getPacketBuffer() *packetBuffer {
	buf := packetBufferStorage.Get()
	buf.refCount = 1
	buf.Data = buf.Data[:0]

	return buf
}

func getLargePacketBuffer() *packetBuffer {
	buf := largePacketBufferStorage.Get()
	buf.refCount = 1
	buf.Data = buf.Data[:0]

	return buf
}
