// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux

package netdial

import (
	"syscall"
)

const (
	soBusyPoll  = 50 // SOL_SOCKET SO_BUSY_POLL
	tcpQuickACK = 12 // IPPROTO_TCP TCP_QUICKACK
)

func applyLinuxSocketOptions(fd uintptr, opts DialOptions) error {
	sfd := int(fd)

	// Enable SO_BUSY_POLL (Kernel network device driver busy polling in microseconds)
	if opts.BusyPollMicroseconds > 0 {
		_ = syscall.SetsockoptInt(sfd, syscall.SOL_SOCKET, soBusyPoll, opts.BusyPollMicroseconds)
	}

	// Enable TCP_QUICKACK (Immediate ACK transmission bypassing delayed ACK timers)
	if opts.TCPQuickACK {
		_ = syscall.SetsockoptInt(sfd, syscall.IPPROTO_TCP, tcpQuickACK, 1)
	}

	// Enable TCP_NODELAY (Disable Nagle's algorithm for instant frame writes)
	_ = syscall.SetsockoptInt(sfd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)

	return nil
}
