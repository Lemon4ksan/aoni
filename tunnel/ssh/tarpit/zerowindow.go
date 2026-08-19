// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tarpit

import (
	"context"
	"net"
	"time"
)

// ZeroWindowFreeze locks an incoming TCP connection into a Layer 4 TCP Zero-Window state.
//
// Mechanism:
// Shrinks the socket receive buffer (SO_RCVBUF) to the minimum system limit and stops calling Read().
// As the remote client transmits data, the OS kernel's TCP stack quickly fills the tiny buffer
// and automatically sends TCP Zero Window ACKs. The client's TCP stack is forced into an
// indefinite Zero Window Probe loop, freezing its socket without consuming CPU on the server.
//
// Protocol Agnostic:
// Works universally across HTTP, HTTPS, SSH, Database protocols, or raw TCP streams.
func ZeroWindowFreeze(ctx context.Context, conn net.Conn, holdDuration time.Duration) {
	defer conn.Close()

	if holdDuration <= 0 {
		holdDuration = 10 * time.Minute
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetReadBuffer(1024)
	}

	var scratch [1024]byte

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Read(scratch[:])
	_ = conn.SetReadDeadline(time.Time{})

	t := time.NewTimer(holdDuration)
	defer t.Stop()

	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
