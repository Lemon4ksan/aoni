// Copyright (c) 2016 the quic-go authors. All rights reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quic

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/quic/internal/protocol"
)

func TestBufferPoolSizes(t *testing.T) {
	buf1 := getPacketBuffer()
	require.Equal(t, protocol.MaxPacketBufferSize, cap(buf1.Data))
	require.Zero(t, buf1.Len())
	buf1.Data = append(buf1.Data, []byte("foobar")...)
	require.Equal(t, protocol.ByteCount(6), buf1.Len())

	buf2 := getLargePacketBuffer()
	require.Equal(t, protocol.MaxLargePacketBufferSize, cap(buf2.Data))
	require.Zero(t, buf2.Len())
}

func TestBufferPoolRelease(t *testing.T) {
	buf1 := getPacketBuffer()
	buf1.Release()
	// panics if released twice
	require.Panics(t, func() { buf1.Release() })

	// panics if wrong-sized buffers are passed
	buf2 := getLargePacketBuffer()
	buf2.Data = make([]byte, 10) // replace the underlying slice
	require.Panics(t, func() { buf2.Release() })
}

func TestBufferPoolSplitting(t *testing.T) {
	buf := getPacketBuffer()
	buf.Split()
	buf.Split()
	// now we have 3 parts
	buf.Decrement()
	buf.Decrement()
	buf.Decrement()
	require.Panics(t, func() { buf.Decrement() })
}

func TestBufferPoolParallel(t *testing.T) {
	t.Parallel()

	const (
		workers    = 32
		iterations = 500
	)

	done := make(chan struct{}, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer func() { done <- struct{}{} }()

			for j := 0; j < iterations; j++ {
				buf := getPacketBuffer()
				buf.Data = append(buf.Data, []byte("data")...)
				buf.Release()

				lbuf := getLargePacketBuffer()
				lbuf.Data = append(lbuf.Data, []byte("largedata")...)
				lbuf.Release()
			}
		}()
	}

	for i := 0; i < workers; i++ {
		<-done
	}
}

func BenchmarkBufferPool_Parallel(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := getPacketBuffer()
			buf.Data = append(buf.Data, 0x01, 0x02, 0x03, 0x04)
			buf.Release()
		}
	})
}
