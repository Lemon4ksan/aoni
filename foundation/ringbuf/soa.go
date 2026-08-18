// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ringbuf

import (
	"errors"
)

// ErrBatchFull is returned when attempting to add items to a full PacketBatchSoA.
var ErrBatchFull = errors.New("aoni/ringbuf: packet batch is full")

// PacketBatchSoA implements a Structure-of-Arrays (SoA) memory layout for high-density
// Layer-3 IP packet and frame batch processing.
//
// Hardware Architecture Benefit:
// Unlike AoS (Array of Structures) which places padded struct fields adjacently,
// SoA organizes memory into parallel contiguous byte slices. When the CPU iterates
// over Protocols, a single 64-byte L1-cache line loads 64 protocol bytes simultaneously,
// eliminating cache line pollution and enabling 0-cache-miss SIMD processing.
type PacketBatchSoA struct {
	Capacity    int
	Len         int
	Protocols   []byte   // Contiguous slice of 1-byte IP protocol IDs (6=TCP, 17=UDP, 1=ICMP)
	StreamIDs   []uint32 // Contiguous slice of 4-byte HTTP/2 / QUIC Stream IDs
	PayloadLens []uint16 // Contiguous slice of 2-byte payload lengths
	Flags       []byte   // Contiguous slice of 1-byte control flags
}

// NewPacketBatchSoA constructs a [PacketBatchSoA] pre-allocated for capacity items.
func NewPacketBatchSoA(capacity int) *PacketBatchSoA {
	if capacity <= 0 {
		capacity = 64
	}

	return &PacketBatchSoA{
		Capacity:    capacity,
		Len:         0,
		Protocols:   make([]byte, 0, capacity),
		StreamIDs:   make([]uint32, 0, capacity),
		PayloadLens: make([]uint16, 0, capacity),
		Flags:       make([]byte, 0, capacity),
	}
}

// Append adds a packet metadata tuple to the batch in SoA layout.
func (b *PacketBatchSoA) Append(proto byte, streamID uint32, payloadLen uint16, flags byte) error {
	if b.Len >= b.Capacity {
		return ErrBatchFull
	}

	b.Protocols = append(b.Protocols, proto)
	b.StreamIDs = append(b.StreamIDs, streamID)
	b.PayloadLens = append(b.PayloadLens, payloadLen)
	b.Flags = append(b.Flags, flags)
	b.Len++

	return nil
}

// Reset clears the batch count while preserving allocated slice capacities.
func (b *PacketBatchSoA) Reset() {
	b.Len = 0
	b.Protocols = b.Protocols[:0]
	b.StreamIDs = b.StreamIDs[:0]
	b.PayloadLens = b.PayloadLens[:0]
	b.Flags = b.Flags[:0]
}

// FilterByProtocol collects all item indices matching targetProto using fast linear scanning.
func (b *PacketBatchSoA) FilterByProtocol(targetProto byte, dstIndices []int) []int {
	dstIndices = dstIndices[:0]
	for i, p := range b.Protocols {
		if p == targetProto {
			dstIndices = append(dstIndices, i)
		}
	}

	return dstIndices
}
