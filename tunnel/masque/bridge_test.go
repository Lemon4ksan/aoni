// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

type bridgeMockAdapter struct {
	name     string
	packets  [][]byte
	writeBuf *bytes.Buffer
	mu       sync.Mutex
	closed   bool
	notify   chan struct{}
}

func newBridgeMockAdapter(name string) *bridgeMockAdapter {
	return &bridgeMockAdapter{
		name:     name,
		writeBuf: new(bytes.Buffer),
		notify:   make(chan struct{}, 100),
	}
}

func (m *bridgeMockAdapter) Name() string {
	return m.name
}

func (m *bridgeMockAdapter) Read(b []byte) (int, error) {
	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()

			return 0, net.ErrClosed
		}

		if len(m.packets) > 0 {
			pkt := m.packets[0]
			m.packets = m.packets[1:]
			n := copy(b, pkt)
			m.mu.Unlock()

			return n, nil
		}

		m.mu.Unlock()

		select {
		case <-m.notify:
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (m *bridgeMockAdapter) Write(b []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return 0, net.ErrClosed
	}

	return m.writeBuf.Write(b)
}

func (m *bridgeMockAdapter) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()

	select {
	case m.notify <- struct{}{}:
	default:
	}

	return nil
}

func (m *bridgeMockAdapter) InjectReadPacket(pkt []byte) {
	m.mu.Lock()
	cp := make([]byte, len(pkt))
	copy(cp, pkt)
	m.packets = append(m.packets, cp)
	m.mu.Unlock()

	select {
	case m.notify <- struct{}{}:
	default:
	}
}

func (m *bridgeMockAdapter) GetWrittenBytes() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.writeBuf.Bytes()
}

func TestBridgeTUN_Default(t *testing.T) {
	t.Parallel()

	adapter := newBridgeMockAdapter("tun_b0")
	clientConn, serverConn := net.Pipe()

	defer clientConn.Close()
	defer serverConn.Close()
	defer adapter.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = BridgeTUN(ctx, adapter, clientConn)
	}()

	packet := make([]byte, 20)
	packet[0] = 0x45
	copy(packet[12:16], netip.MustParseAddr("10.0.0.5").AsSlice())
	copy(packet[16:20], netip.MustParseAddr("1.1.1.1").AsSlice())

	adapter.InjectReadPacket(packet)

	readBuf := make([]byte, 1024)
	n, err := serverConn.Read(readBuf)
	require.NoError(t, err)
	assert.Equal(t, packet, readBuf[:n])

	cancel()
}

func TestBridgeTUNWithOptions_IngressFiltering(t *testing.T) {
	t.Parallel()

	adapter := newBridgeMockAdapter("tun_b1")
	clientConn, serverConn := net.Pipe()

	defer clientConn.Close()
	defer serverConn.Close()
	defer adapter.Close()

	opts := BridgeOptions{
		AllowedPrefixes: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/16"),
		},
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = BridgeTUNWithOptions(ctx, adapter, clientConn, opts)
	}()

	// Packet 1: Spoofed IP 192.168.1.100 (should be dropped by uRPF)
	spoofedPkt := make([]byte, 20)
	spoofedPkt[0] = 0x45
	copy(spoofedPkt[12:16], netip.MustParseAddr("192.168.1.100").AsSlice())
	copy(spoofedPkt[16:20], netip.MustParseAddr("1.1.1.1").AsSlice())

	// Packet 2: Valid IP 10.0.0.5 (should pass uRPF filter)
	validPkt := make([]byte, 20)
	validPkt[0] = 0x45
	copy(validPkt[12:16], netip.MustParseAddr("10.0.0.5").AsSlice())
	copy(validPkt[16:20], netip.MustParseAddr("1.1.1.1").AsSlice())

	adapter.InjectReadPacket(spoofedPkt)
	adapter.InjectReadPacket(validPkt)

	readBuf := make([]byte, 1024)
	n, err := serverConn.Read(readBuf)
	require.NoError(t, err)
	// Must receive validPkt, not spoofedPkt
	assert.Equal(t, validPkt, readBuf[:n])

	cancel()
}

func TestBridgeTUNWithOptions_MTULimit(t *testing.T) {
	t.Parallel()

	adapter := newBridgeMockAdapter("tun_b2")
	clientConn, serverConn := net.Pipe()

	defer clientConn.Close()
	defer serverConn.Close()
	defer adapter.Close()

	opts := BridgeOptions{
		MaxMTU: 1300,
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = BridgeTUNWithOptions(ctx, adapter, clientConn, opts)
	}()

	// Oversized packet (1400 bytes > MaxMTU 1300)
	overPkt := make([]byte, 1400)
	overPkt[0] = 0x45
	copy(overPkt[12:16], netip.MustParseAddr("10.0.0.5").AsSlice())
	copy(overPkt[16:20], netip.MustParseAddr("1.1.1.1").AsSlice())

	adapter.InjectReadPacket(overPkt)

	var written []byte
	require.Eventually(t, func() bool {
		written = adapter.GetWrittenBytes()
		return len(written) > 0
	}, time.Second, 10*time.Millisecond)

	// Adapter should have received ICMP Packet Too Big response
	assert.Equal(t, byte(0x45), written[0])
	assert.Equal(t, byte(3), written[20]) // ICMP Type 3
	assert.Equal(t, byte(4), written[21]) // ICMP Code 4
	assert.Equal(t, uint16(1300), binary.BigEndian.Uint16(written[26:28]))

	cancel()
}
