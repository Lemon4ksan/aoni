// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build unix

package sysnet

import (
	"syscall"
)

func tuneSocketFD(fd uintptr) {
	_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
}

func tuneSocketFlagsFD(fd uintptr, flags uint64) {
	_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)

	// ExpTCPFastOpen (bitmask flag 1<<4)
	if flags&(1<<4) != 0 {
		_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, 30 /* TCP_FASTOPEN_CONNECT */, 1)
	}

	// ExpBusyPoll (bitmask flag 1<<5)
	if flags&(1<<5) != 0 {
		_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, 46 /* SO_BUSY_POLL */, 50)
	}
}
