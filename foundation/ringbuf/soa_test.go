// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ringbuf_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/foundation/ringbuf"
)

type PacketAoS struct {
	StreamID   uint32
	PayloadLen uint16
	Protocol   byte
	Flags      byte
}

func TestPacketBatchSoA_Operations(t *testing.T) {
	t.Parallel()

	batch := ringbuf.NewPacketBatchSoA(4)
	assert.Equal(t, 4, batch.Capacity)
	assert.Equal(t, 0, batch.Len)

	require.NoError(t, batch.Append(6, 101, 1420, 0x01))
	require.NoError(t, batch.Append(17, 103, 512, 0x00))
	require.NoError(t, batch.Append(6, 105, 1280, 0x01))
	require.NoError(t, batch.Append(1, 107, 64, 0x00))

	assert.Equal(t, 4, batch.Len)
	assert.ErrorIs(t, batch.Append(6, 109, 100, 0x00), ringbuf.ErrBatchFull)

	indices := batch.FilterByProtocol(6, nil)
	assert.Equal(t, []int{0, 2}, indices)

	batch.Reset()
	assert.Equal(t, 0, batch.Len)
}

func BenchmarkBatch_SoA_Vs_AoS(b *testing.B) {
	const batchSize = 1024

	soa := ringbuf.NewPacketBatchSoA(batchSize)
	aos := make([]PacketAoS, batchSize)

	for i := 0; i < batchSize; i++ {
		proto := byte(6)
		if i%2 == 0 {
			proto = 17
		}

		_ = soa.Append(proto, uint32(i), uint16(i*10), 0x01)
		aos[i] = PacketAoS{
			Protocol:   proto,
			StreamID:   uint32(i),
			PayloadLen: uint16(i * 10),
			Flags:      0x01,
		}
	}

	b.Run("SoA_FilterProtocol", func(b *testing.B) {
		b.ReportAllocs()

		indices := make([]int, 0, batchSize)
		for i := 0; i < b.N; i++ {
			indices = soa.FilterByProtocol(6, indices)
		}
	})

	b.Run("AoS_FilterProtocol", func(b *testing.B) {
		b.ReportAllocs()

		indices := make([]int, 0, batchSize)
		for i := 0; i < b.N; i++ {
			indices = indices[:0]
			for idx, item := range aos {
				if item.Protocol == 6 {
					indices = append(indices, idx)
				}
			}
		}
	})
}
