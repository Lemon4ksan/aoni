// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ringbuf_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/ringbuf"
)

func TestRingBufferPushPop(t *testing.T) {
	rb := ringbuf.NewRingBuffer[string](16)

	val1 := "entry_1"
	val2 := "entry_2"

	assert.True(t, rb.Push(&val1))
	assert.True(t, rb.Push(&val2))

	assert.Equal(t, &val1, rb.Pop())
	assert.Equal(t, &val2, rb.Pop())
	assert.Nil(t, rb.Pop())
}

func TestRingBufferConcurrent(t *testing.T) {
	rb := ringbuf.NewRingBuffer[int](2048)

	var wg sync.WaitGroup

	numProducers := 16
	itemsPerProducer := 100

	for i := 0; i < numProducers; i++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			for j := 0; j < itemsPerProducer; j++ {
				val := id*1000 + j
				for !rb.Push(&val) {
					// Retry if full
				}
			}
		}(i)
	}

	wg.Wait()
	assert.Equal(t, numProducers*itemsPerProducer, rb.Len())
}

func TestRingBufferConcurrentProduceConsume(t *testing.T) {
	rb := ringbuf.NewRingBuffer[int](128)

	var producerWg sync.WaitGroup

	numProducers := 8
	itemsPerProducer := 1000

	doneProducing := make(chan struct{})

	for i := 0; i < numProducers; i++ {
		producerWg.Add(1)

		go func(id int) {
			defer producerWg.Done()

			for j := 0; j < itemsPerProducer; j++ {
				val := id*10000 + j
				for !rb.Push(&val) {
					// Retry if full
				}
			}
		}(i)
	}

	go func() {
		producerWg.Wait()
		close(doneProducing)
	}()

	var consumeWg sync.WaitGroup

	for i := 0; i < 4; i++ {
		consumeWg.Add(1)

		go func() {
			defer consumeWg.Done()

			for {
				item := rb.Pop()
				if item != nil {
					_ = *item
				} else {
					select {
					case <-doneProducing:
						if rb.Len() == 0 {
							return
						}
					default:
					}
				}
			}
		}()
	}

	consumeWg.Wait()
}
