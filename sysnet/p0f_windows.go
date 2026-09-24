// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package sysnet

import "syscall"

// ApplyP0fSignature applies OS socket TTL, DF, and buffer size flags for p0f fingerprinting.
func ApplyP0fSignature(raw syscall.RawConn, ttl, windowSize int, setWindow, hasDF bool) {
	if ttl > 0 {
		_ = raw.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, syscall.IP_TTL, ttl)
		})
	}

	if hasDF {
		_ = raw.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, 14, 1)
		})
	}

	if setWindow && windowSize > 0 {
		_ = raw.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, windowSize)
		})
	}
}
