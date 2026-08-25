// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import (
	"context"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"
)

type pairedDatagramTransport struct {
	sendCh chan []byte
	recvCh chan []byte
	closed atomic.Bool
}

func newDatagramPair() (*pairedDatagramTransport, *pairedDatagramTransport) {
	c1 := make(chan []byte, 200)
	c2 := make(chan []byte, 200)

	return &pairedDatagramTransport{sendCh: c1, recvCh: c2}, &pairedDatagramTransport{sendCh: c2, recvCh: c1}
}

func (p *pairedDatagramTransport) SendDatagram(b []byte) error {
	if p.closed.Load() {
		return net.ErrClosed
	}

	cp := make([]byte, len(b))
	copy(cp, b)

	select {
	case p.sendCh <- cp:
		return nil
	default:
		return nil
	}
}

func (p *pairedDatagramTransport) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case data, ok := <-p.recvCh:
		if !ok {
			return nil, net.ErrClosed
		}

		return data, nil
	}
}

func createConnectedSessions() (clientSession, serverSession *Session) {
	clientConn, serverConn := net.Pipe()
	dgramClient, dgramServer := newDatagramPair()

	clientSession = NewSession(clientConn, dgramClient)
	serverSession = NewSession(serverConn, dgramServer)

	return clientSession, serverSession
}

func TestServer_ClientConnectAndEcho(t *testing.T) {
	t.Parallel()

	srv := NewServer(ServerConfig{
		IPv4Prefix: netip.MustParsePrefix("10.8.0.0/24"),
		PacketHandler: func(packet []byte, _ netip.Addr, reply func(packet []byte) error) error {
			// Echo response: swap src/dst and reply
			if len(packet) >= 20 && (packet[0]>>4) == 4 {
				echoPkt := make([]byte, len(packet))
				copy(echoPkt, packet)
				// Swap IPv4 src and dst
				copy(echoPkt[12:16], packet[16:20])
				copy(echoPkt[16:20], packet[12:16])

				return reply(echoPkt)
			}

			return nil
		},
	})
	defer srv.Close()

	clientSess, serverSess := createConnectedSessions()
	defer clientSess.Close()
	defer serverSess.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = srv.HandleSession(ctx, serverSess)
	}()

	// 1. Client receives ADDRESS_ASSIGN
	cType, payload, err := clientSess.ReadCapsule()
	require.NoError(t, err)
	assert.Equal(t, CapsuleAddressAssign, cType)

	assignedList, err := DecodeAddressAssignPayload(payload)
	require.NoError(t, err)
	require.Len(t, assignedList, 1)

	clientIP := assignedList[0].Addr
	assert.Equal(t, "10.8.0.2", clientIP.String())
	assert.Equal(t, 1, srv.ActiveSessions())

	// 2. Client sends IP packet (from clientIP to 8.8.8.8)
	reqPkt := make([]byte, 20)
	reqPkt[0] = 0x45
	copy(reqPkt[12:16], clientIP.AsSlice())
	copy(reqPkt[16:20], netip.MustParseAddr("8.8.8.8").AsSlice())

	err = clientSess.SendIPPacket(reqPkt)
	require.NoError(t, err)

	// 3. Client receives echo reply (from 8.8.8.8 to clientIP)
	recvCtx, recvCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer recvCancel()

	respPkt, err := clientSess.ReceiveIPPacket(recvCtx)
	require.NoError(t, err)
	require.Len(t, respPkt, 20)

	assert.Equal(t, netip.MustParseAddr("8.8.8.8"), ExtractSrcIP(respPkt))
	assert.Equal(t, clientIP, ExtractDestIP(respPkt))
}

func TestServer_ClientToClientRouting(t *testing.T) {
	t.Parallel()

	srv := NewServer(ServerConfig{
		IPv4Prefix: netip.MustParsePrefix("10.8.0.0/24"),
	})
	defer srv.Close()

	clientA, serverA := createConnectedSessions()
	defer clientA.Close()
	defer serverA.Close()

	clientB, serverB := createConnectedSessions()
	defer clientB.Close()
	defer serverB.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() { _ = srv.HandleSession(ctx, serverA) }()
	go func() { _ = srv.HandleSession(ctx, serverB) }()

	// Receive assignments
	_, pA, err := clientA.ReadCapsule()
	require.NoError(t, err)
	assignedA, err := DecodeAddressAssignPayload(pA)
	require.NoError(t, err)

	_, pB, err := clientB.ReadCapsule()
	require.NoError(t, err)
	assignedB, err := DecodeAddressAssignPayload(pB)
	require.NoError(t, err)

	ipA := assignedA[0].Addr
	ipB := assignedB[0].Addr
	assert.NotEqual(t, ipA, ipB)
	assert.Equal(t, 2, srv.ActiveSessions())

	// Client A sends packet directly to Client B's IP
	pktA2B := make([]byte, 20)
	pktA2B[0] = 0x45
	copy(pktA2B[12:16], ipA.AsSlice())
	copy(pktA2B[16:20], ipB.AsSlice())

	err = clientA.SendIPPacket(pktA2B)
	require.NoError(t, err)

	recvCtx, recvCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer recvCancel()

	receivedByB, err := clientB.ReceiveIPPacket(recvCtx)
	require.NoError(t, err)
	assert.Equal(t, pktA2B, receivedByB)
}
