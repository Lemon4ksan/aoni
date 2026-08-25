// Copyright (c) 2016 the quic-go authors. All rights reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package quic

import (
	"net"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/internal/quic/internal/utils"
)

func TestClosedLocalConnection(t *testing.T) {
	written := make(chan net.Addr, 1)
	conn := newClosedLocalConn(func(addr net.Addr, _ packetInfo) { written <- addr }, utils.DefaultLogger)

	addr := &net.UDPAddr{IP: net.IPv4(127, 1, 2, 3), Port: 1337}
	for i := 1; i <= 20; i++ {
		conn.handlePacket(receivedPacket{remoteAddr: addr})

		if i == 1 || i == 2 || i == 4 || i == 8 || i == 16 {
			select {
			case gotAddr := <-written:
				require.Equal(t, addr, gotAddr) // receive the CONNECTION_CLOSE
			default:
				t.Fatal("expected to receive address")
			}
		} else {
			select {
			case gotAddr := <-written:
				t.Fatalf("unexpected address received: %v", gotAddr)
			default:
				// Nothing received, which is expected
			}
		}
	}
}
