// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package processor_test

import (
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/realtime/socket"
	"github.com/lemon4ksan/aoni/realtime/socket/processor"
)

type testPacket struct {
	Body string
}

func TestProcessor_WorkerPoolParallelParsing(t *testing.T) {
	t.Parallel()

	inputCh := make(chan *socket.FrameBuffer, 100)

	var (
		mu       sync.Mutex
		received []string
	)

	consumer := processor.ConsumerFunc[*testPacket](func(pkt *testPacket) bool {
		mu.Lock()

		received = append(received, pkt.Body)
		mu.Unlock()

		return true
	})

	decode := func(data []byte) (*testPacket, error) {
		return &testPacket{Body: string(data)}, nil
	}

	proc := processor.New[*testPacket](
		processor.Config{WorkerCount: 4},
		inputCh,
		consumer,
		decode,
	)

	proc.Start()
	defer proc.Stop()

	// Enqueue frames
	for i := 0; i < 20; i++ {
		fb := socket.AcquireFrameBuffer(5)
		copy(fb.Bytes(), []byte("msg"))

		inputCh <- fb
	}

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) == 20
	}, 1*time.Second, 10*time.Millisecond)

	mu.Lock()
	assert.Equal(t, 20, len(received))
	mu.Unlock()
}
