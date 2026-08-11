// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package sysnet

import "golang.org/x/sys/windows"

func tuneSocketFD(fd uintptr) {
	_ = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_TCP, windows.TCP_NODELAY, 1)
}

func tuneSocketFlagsFD(fd uintptr, flags uint64) {
	_ = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_TCP, windows.TCP_NODELAY, 1)
}
