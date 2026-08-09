// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build darwin || freebsd || openbsd

package sysnet

import (
	"syscall"
	"unsafe"
)

// ApplyP0fSignature applies OS socket TTL, DF, and buffer size flags for p0f fingerprinting.
func ApplyP0fSignature(raw syscall.RawConn, ttl, windowSize int, setWindow, hasDF bool) {
	if ttl > 0 {
		_ = raw.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_TTL, ttl) //nolint:gosec
		})
	}

	if hasDF {
		_ = raw.Control(func(fd uintptr) {
			val := 1
			_, _, _ = syscall.Syscall6(
				syscall.SYS_SETSOCKOPT,
				fd,
				uintptr(syscall.IPPROTO_IP),
				uintptr(27), // IP_DONTFRAG on macOS
				uintptr(unsafe.Pointer(&val)),
				unsafe.Sizeof(int32(0)),
				0,
			)
		})
	}

	if setWindow && windowSize > 0 {
		_ = raw.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, windowSize) //nolint:gosec
		})
	}
}
