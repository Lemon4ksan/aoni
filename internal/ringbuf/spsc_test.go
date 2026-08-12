// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ringbuf_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/ringbuf"
)

func TestSPSCRingBuffer_BasicOperations(t *testing.T) {
	t.Parallel()

	buf := ringbuf.NewSPSCRingBuffer[int](4)
	assert.Equal(t, 4, buf.Cap())
	assert.Equal(t, 0, buf.Len())
	assert.True(t, buf.IsEmpty())
	assert.False(t, buf.IsFull())

	v1, v2, v3, v4, v5 := 10, 20, 30, 40, 50

	require.True(t, buf.Push(&v1))
	require.True(t, buf.Push(&v2))
	require.True(t, buf.Push(&v3))
	require.True(t, buf.Push(&v4))
	assert.True(t, buf.IsFull())

	// Push when full should fail
	require.False(t, buf.Push(&v5))

	// Pop elements in FIFO order
	p1 := buf.Pop()
	require.NotNil(t, p1)
	assert.Equal(t, 10, *p1)

	p2 := buf.Pop()
	require.NotNil(t, p2)
	assert.Equal(t, 20, *p2)

	assert.Equal(t, 2, buf.Len())

	// Push after pop should succeed
	require.True(t, buf.Push(&v5))

	p3 := buf.Pop()
	require.NotNil(t, p3)
	assert.Equal(t, 30, *p3)

	p4 := buf.Pop()
	require.NotNil(t, p4)
	assert.Equal(t, 40, *p4)

	p5 := buf.Pop()
	require.NotNil(t, p5)
	assert.Equal(t, 50, *p5)

	assert.True(t, buf.IsEmpty())
	assert.Nil(t, buf.Pop())
}

func TestSPSCRingBuffer_ConcurrentProducerConsumer(t *testing.T) {
	t.Parallel()

	const itemCount = 100_000

	buf := ringbuf.NewSPSCRingBuffer[int](1024)

	var wg sync.WaitGroup

	wg.Add(2)

	// Single Producer Goroutine
	go func() {
		defer wg.Done()

		for i := 0; i < itemCount; i++ {
			val := i

			for !buf.Push(&val) {
				// Spin until space is available
			}
		}
	}()

	received := make([]int, 0, itemCount)

	// Single Consumer Goroutine
	go func() {
		defer wg.Done()

		for len(received) < itemCount {
			item := buf.Pop()
			if item != nil {
				received = append(received, *item)
			}
		}
	}()

	wg.Wait()

	require.Len(t, received, itemCount)

	for i := 0; i < itemCount; i++ {
		assert.Equal(t, i, received[i])
	}
}

func BenchmarkSPSCRingBuffer_PushPop(b *testing.B) {
	buf := ringbuf.NewSPSCRingBuffer[int](1024)
	val := 42

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		buf.Push(&val)
		_ = buf.Pop()
	}
}
