// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTUNAdapter struct {
	name     string
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	mu       sync.Mutex
	readErr  error
	writeErr error
	closed   bool
}

func newMockTUNAdapter(name string) *mockTUNAdapter {
	return &mockTUNAdapter{
		name:     name,
		readBuf:  new(bytes.Buffer),
		writeBuf: new(bytes.Buffer),
	}
}

func (m *mockTUNAdapter) Name() string {
	return m.name
}

func (m *mockTUNAdapter) Read(b []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.readErr != nil {
		return 0, m.readErr
	}

	if m.closed {
		return 0, io.EOF
	}

	if m.readBuf.Len() == 0 {
		m.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
		m.mu.Lock()

		if m.readBuf.Len() == 0 {
			return 0, nil
		}
	}

	return m.readBuf.Read(b)
}

func (m *mockTUNAdapter) Write(b []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.writeErr != nil {
		return 0, m.writeErr
	}

	if m.closed {
		return 0, io.ErrClosedPipe
	}

	return m.writeBuf.Write(b)
}

func (m *mockTUNAdapter) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true

	return nil
}

func (m *mockTUNAdapter) InjectReadPacket(packet []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.readBuf.Write(packet)
}

func (m *mockTUNAdapter) GetWrittenBytes() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.writeBuf.Bytes()
}

func TestBridgeTUNToMASQUE_Bidirectional(t *testing.T) {
	t.Parallel()

	adapter := newMockTUNAdapter("tun_test0")
	clientConn, serverConn := net.Pipe()

	defer clientConn.Close()
	defer serverConn.Close()
	defer adapter.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- BridgeTUN(ctx, adapter, clientConn)
	}()

	// Test 1: OS Kernel (Adapter) -> MASQUE Proxy (Conn)
	packetFromKernel := []byte{0x45, 0x00, 0x00, 0x14, 0x00, 0x01} // Sample IP packet
	adapter.InjectReadPacket(packetFromKernel)

	readBuf := make([]byte, 1024)
	n, err := serverConn.Read(readBuf)
	require.NoError(t, err)
	assert.Equal(t, packetFromKernel, readBuf[:n])

	// Test 2: MASQUE Proxy (Conn) -> OS Kernel (Adapter)
	packetFromMASQUE := []byte{0x45, 0x00, 0x00, 0x20, 0x00, 0x02}
	_, err = serverConn.Write(packetFromMASQUE)
	require.NoError(t, err)

	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, packetFromMASQUE, adapter.GetWrittenBytes())

	// Cancel context to stop bridge
	cancel()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(1 * time.Second):
		t.Fatal("BridgeTUNT did not exit promptly after context cancellation")
	}
}

func TestBridgeTUNToMASQUE_AdapterReadError(t *testing.T) {
	t.Parallel()

	adapter := newMockTUNAdapter("tun_err0")
	adapter.readErr = errors.New("read error simulation")
	clientConn, serverConn := net.Pipe()

	defer clientConn.Close()
	defer serverConn.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	err := BridgeTUN(ctx, adapter, clientConn)
	require.NoError(t, err)
}

func TestBridgeTUNToMASQUE_ConnReadError(t *testing.T) {
	t.Parallel()

	adapter := newMockTUNAdapter("tun_err1")
	clientConn, serverConn := net.Pipe()

	_ = serverConn.Close() // Close server side immediately to cause read error on clientConn

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	err := BridgeTUN(ctx, adapter, clientConn)
	require.NoError(t, err)

	_ = clientConn.Close()
}
