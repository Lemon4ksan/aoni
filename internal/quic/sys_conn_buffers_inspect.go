// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build darwin || freebsd || linux || openbsd

package quic

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func inspectReadBuffer(c syscall.RawConn) (int, error) {
	var (
		size int
		serr error
	)

	if err := c.Control(func(fd uintptr) {
		size, serr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUF)
	}); err != nil {
		return 0, err
	}

	return size, serr
}

func inspectWriteBuffer(c syscall.RawConn) (int, error) {
	var (
		size int
		serr error
	)

	if err := c.Control(func(fd uintptr) {
		size, serr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_SNDBUF)
	}); err != nil {
		return 0, err
	}

	return size, serr
}
