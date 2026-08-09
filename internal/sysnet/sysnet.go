// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sysnet provides low-level OS socket syscall overrides via syscall.RawConn,
// configuring TCP_QUICKACK, TCP_NODELAY, and socket buffer parameters to minimize network tail latency.
package sysnet

import (
	"net"
)

// TuneSocketConn applies low-latency OS syscall flags (TCP_NODELAY and socket buffer tuning) to a [net.Conn].
func TuneSocketConn(conn net.Conn) {
	if conn == nil {
		return
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
		_ = tcpConn.SetKeepAlive(true)

		rawConn, err := tcpConn.SyscallConn()
		if err != nil {
			return
		}

		_ = rawConn.Control(func(fd uintptr) {
			tuneSocketFD(fd)
		})
	}
}
