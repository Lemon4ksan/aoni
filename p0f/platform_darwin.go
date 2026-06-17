// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build darwin || freebsd || openbsd

package p0f

import (
	"syscall"
	"unsafe"
)

func setDF(fd uintptr, enable bool) error {
	val := 0
	if enable {
		val = 1
	}
	_, _, errno := syscall.Syscall6(
		syscall.SYS_SETSOCKOPT,
		fd,
		uintptr(syscall.IPPROTO_IP),
		uintptr(27), // IP_DONTFRAG on macOS
		uintptr(unsafe.Pointer(&val)),
		unsafe.Sizeof(int32(0)),
		0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func applySignature(raw syscall.RawConn, sig *Signature) {
	// Set TTL
	if sig.TTL > 0 {
		raw.Control(func(fd uintptr) {
			syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_TTL, sig.TTL)
		})
	}

	// Set DF flag (Don't Fragment) - IP_DONTFRAG = 27 on macOS
	if hasQuirk(sig.Quirks, "df") || hasQuirk(sig.Quirks, "df+") {
		raw.Control(func(fd uintptr) {
			setDF(fd, true)
		})
	}

	// Influence window size via SO_RCVBUF
	if sig.WindowType == WindowNormal && sig.WindowSize > 0 {
		raw.Control(func(fd uintptr) {
			syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, sig.WindowSize)
		})
	}
}
