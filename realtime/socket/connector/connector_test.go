// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package connector_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/realtime/socket"
	"github.com/lemon4ksan/aoni/realtime/socket/connector"
)

type mockConnection struct {
	sent     [][]byte
	incoming chan *socket.FrameBuffer
	closed   bool
}

func (m *mockConnection) Send(_ context.Context, data []byte) error {
	m.sent = append(m.sent, data)
	return nil
}

func (m *mockConnection) Receive(ctx context.Context) (*socket.FrameBuffer, error) {
	select {
	case fb, ok := <-m.incoming:
		if !ok {
			return nil, errors.New("closed")
		}

		return fb, nil

	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *mockConnection) Close() error {
	m.closed = true
	return nil
}

func TestConnector_ConnectAndSendReceive(t *testing.T) {
	t.Parallel()

	mockConn := &mockConnection{
		incoming: make(chan *socket.FrameBuffer, 10),
	}

	dialer := func(_ context.Context, endpoint string, _ socket.Framer, _ socket.Cipher) (connector.Connection, error) {
		assert.Equal(t, "127.0.0.1:8080", endpoint)
		return mockConn, nil
	}

	cfg := connector.Config[string]{
		Dialer: dialer,
	}

	conn := connector.New[string](cfg)
	defer func() { _ = conn.Close() }()

	err := conn.Connect(t.Context(), "127.0.0.1:8080")
	require.NoError(t, err)
	assert.True(t, conn.IsConnected())

	// Test Send
	err = conn.Send(t.Context(), []byte("ping"))
	require.NoError(t, err)
	assert.Equal(t, 1, len(mockConn.sent))
	assert.Equal(t, []byte("ping"), mockConn.sent[0])

	// Test Inbound Frame
	fb := socket.AcquireFrameBuffer(4)
	copy(fb.Bytes(), []byte("pong"))

	mockConn.incoming <- fb

	select {
	case received := <-conn.C():
		assert.Equal(t, []byte("pong"), received.Bytes())
		socket.ReleaseFrameBuffer(received)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for incoming frame")
	}

	// Test Disconnect
	require.NoError(t, conn.Disconnect())
	assert.False(t, conn.IsConnected())
}

func TestConnector_NetConnWrapper(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	framer := socket.NewLengthPrefixedFramer(socket.LengthPrefixedConfig{})
	wrapper := connector.NewNetConnWrapper(client, framer, nil)

	go func() {
		_ = wrapper.Send(t.Context(), []byte("hello pipe"))
	}()

	readFB, err := framer.ReadFrame(server)
	require.NoError(t, err)

	defer socket.ReleaseFrameBuffer(readFB)

	assert.Equal(t, []byte("hello pipe"), readFB.Bytes())
}
