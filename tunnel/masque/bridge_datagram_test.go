// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"context"
	"encoding/binary"
	"net/netip"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

func TestBridgeTUNDatagram_NilArgs(t *testing.T) {
	t.Parallel()

	assert.Error(t, BridgeTUNDatagram(t.Context(), nil, nil, BridgeOptions{}))
	assert.Error(t, BridgeTUNDatagram(t.Context(), newBridgeMockAdapter("t0"), nil, BridgeOptions{}))
}

func TestBridgeTUNDatagram_BasicForwarding(t *testing.T) {
	t.Parallel()

	adapter := newBridgeMockAdapter("tun_d0")
	stream := newMockStream()
	dgrams := newMockDatagramTransport()
	sess := NewSession(stream, dgrams)

	defer adapter.Close()
	defer sess.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = BridgeTUNDatagram(ctx, adapter, sess, BridgeOptions{})
	}()

	// 1. Packet from TUN -> Datagram
	pkt := []byte{0x45, 0x00, 0x00, 0x14, 0x00, 0x01, 0x00, 0x00, 0x40, 0x06, 0x00, 0x00, 10, 0, 0, 5, 1, 1, 1, 1}
	adapter.InjectReadPacket(pkt)

	require.Eventually(t, func() bool {
		dgrams.mu.Lock()
		defer dgrams.mu.Unlock()
		return len(dgrams.sent) > 0
	}, time.Second, 10*time.Millisecond)

	dgrams.mu.Lock()
	sent := dgrams.sent[0]
	dgrams.mu.Unlock()

	assert.Equal(t, byte(0x00), sent[0]) // Context ID 0
	assert.Equal(t, pkt, sent[1:])

	// 2. Packet from Datagram -> TUN
	respPkt := []byte{0x45, 0x00, 0x00, 0x14, 0x00, 0x02, 0x00, 0x00, 0x40, 0x06, 0x00, 0x00, 1, 1, 1, 1, 10, 0, 0, 5}
	dgrams.InjectDatagram(append([]byte{0x00}, respPkt...))

	require.Eventually(t, func() bool {
		written := adapter.GetWrittenBytes()
		return len(written) >= len(respPkt)
	}, time.Second, 10*time.Millisecond)

	written := adapter.GetWrittenBytes()
	assert.Equal(t, respPkt, written[:len(respPkt)])
}

func TestBridgeTUNDatagram_IngressFilterAndMTU(t *testing.T) {
	t.Parallel()

	adapter := newBridgeMockAdapter("tun_d1")
	stream := newMockStream()
	dgrams := newMockDatagramTransport()
	sess := NewSession(stream, dgrams)

	defer adapter.Close()
	defer sess.Close()

	opts := BridgeOptions{
		AllowedPrefixes: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/16"),
		},
		MaxMTU: 1300,
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = BridgeTUNDatagram(ctx, adapter, sess, opts)
	}()

	// Spoofed packet (192.168.1.100) -> dropped by uRPF
	spoofedPkt := make([]byte, 20)
	spoofedPkt[0] = 0x45
	copy(spoofedPkt[12:16], netip.MustParseAddr("192.168.1.100").AsSlice())
	copy(spoofedPkt[16:20], netip.MustParseAddr("1.1.1.1").AsSlice())
	adapter.InjectReadPacket(spoofedPkt)

	// Valid packet (10.0.0.5) -> passed
	validPkt := make([]byte, 20)
	validPkt[0] = 0x45
	copy(validPkt[12:16], netip.MustParseAddr("10.0.0.5").AsSlice())
	copy(validPkt[16:20], netip.MustParseAddr("1.1.1.1").AsSlice())
	adapter.InjectReadPacket(validPkt)

	require.Eventually(t, func() bool {
		dgrams.mu.Lock()
		defer dgrams.mu.Unlock()
		return len(dgrams.sent) == 1
	}, time.Second, 10*time.Millisecond)

	dgrams.mu.Lock()
	sent := dgrams.sent[0]
	dgrams.mu.Unlock()
	assert.Equal(t, validPkt, sent[1:])

	// Oversized packet -> triggers ICMP PTB
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

	assert.Equal(t, byte(0x45), written[0])
	assert.Equal(t, byte(3), written[20]) // ICMP Type 3
	assert.Equal(t, byte(4), written[21]) // ICMP Code 4
	assert.Equal(t, uint16(1300), binary.BigEndian.Uint16(written[26:28]))
}
