// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dispatcher_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/realtime/socket/dispatcher"
)

type dummyPacket struct {
	Op     int
	Method string
	JobID  uint64
	Body   string
}

type mockWriter struct {
	mu     sync.Mutex
	sent   [][]byte
	onSend func(data []byte)
}

func (m *mockWriter) Send(_ context.Context, data []byte) error {
	m.mu.Lock()
	m.sent = append(m.sent, data)
	fn := m.onSend
	m.mu.Unlock()

	if fn != nil {
		fn(data)
	}

	return nil
}

func TestDispatcher_OpCodeAndMethodRouting(t *testing.T) {
	t.Parallel()

	writer := &mockWriter{}
	extractor := dispatcher.Extractor[int, uint64, *dummyPacket]{
		GetOpCode: func(p *dummyPacket) int { return p.Op },
		GetMethod: func(p *dummyPacket) (string, bool) { return p.Method, p.Method != "" },
		GetJobID:  func(p *dummyPacket) (uint64, bool) { return p.JobID, p.JobID != 0 },
	}

	disp := dispatcher.New[int, uint64, *dummyPacket](
		dispatcher.Config{MaxJobs: 100},
		writer,
		extractor,
	)
	defer func() { _ = disp.Close() }()

	var (
		mu         sync.Mutex
		opCount    int
		methodName string
	)

	disp.RegisterHandler(42, func(p *dummyPacket) {
		mu.Lock()
		opCount++
		mu.Unlock()
	})

	disp.RegisterMethodHandler("UserService.GetProfile", func(p *dummyPacket) {
		mu.Lock()
		methodName = p.Method
		mu.Unlock()
	})

	// Dispatch OpCode 42
	handled := disp.Dispatch(&dummyPacket{Op: 42, Body: "op"})
	assert.True(t, handled)

	// Dispatch Method
	handled = disp.Dispatch(&dummyPacket{Method: "UserService.GetProfile", Body: "profile"})
	assert.True(t, handled)

	// Dispatch Unhandled
	handled = disp.Dispatch(&dummyPacket{Op: 999})
	assert.False(t, handled)

	mu.Lock()
	assert.Equal(t, 1, opCount)
	assert.Equal(t, "UserService.GetProfile", methodName)
	mu.Unlock()
}

func TestDispatcher_SendSyncJobCorrelation(t *testing.T) {
	t.Parallel()

	writer := &mockWriter{}
	extractor := dispatcher.Extractor[int, uint64, *dummyPacket]{
		GetOpCode: func(p *dummyPacket) int { return p.Op },
		GetJobID:  func(p *dummyPacket) (uint64, bool) { return p.JobID, p.JobID != 0 },
	}

	disp := dispatcher.New[int, uint64, *dummyPacket](
		dispatcher.Config{MaxJobs: 100},
		writer,
		extractor,
	)
	defer func() { _ = disp.Close() }()

	jobID := disp.NextJobID()

	// Simulate server replying to jobID in background
	writer.onSend = func(_ []byte) {
		go func() {
			time.Sleep(10 * time.Millisecond)
			disp.Dispatch(&dummyPacket{
				JobID: jobID,
				Body:  "sync response data",
			})
		}()
	}

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	resp, err := disp.SendSync(ctx, jobID, []byte("request payload"))
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "sync response data", resp.Body)
}
