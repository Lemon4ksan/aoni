// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux

package netdial

import "golang.org/x/sys/unix"

const (
	soBusyPoll  = 50 // SOL_SOCKET SO_BUSY_POLL
	tcpQuickACK = 12 // IPPROTO_TCP TCP_QUICKACK
)

func applyLinuxSocketOptions(fd uintptr, opts DialOptions) error {
	sfd := int(fd)

	if opts.InterfaceName != "" {
		_ = unix.SetsockoptString(sfd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, opts.InterfaceName)
	}

	if opts.SocketMark > 0 {
		_ = unix.SetsockoptInt(sfd, unix.SOL_SOCKET, unix.SO_MARK, int(opts.SocketMark))
	}

	if opts.BusyPollMicroseconds > 0 {
		_ = unix.SetsockoptInt(sfd, unix.SOL_SOCKET, soBusyPoll, opts.BusyPollMicroseconds)
	}

	if opts.TCPQuickACK {
		_ = unix.SetsockoptInt(sfd, unix.IPPROTO_TCP, tcpQuickACK, 1)
	}

	_ = unix.SetsockoptInt(sfd, unix.IPPROTO_TCP, unix.TCP_NODELAY, 1)

	return nil
}
