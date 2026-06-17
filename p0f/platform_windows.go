// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package p0f

import "syscall"

func applySignature(raw syscall.RawConn, sig *Signature) {
	// Set TTL
	if sig.TTL > 0 {
		_ = raw.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, syscall.IP_TTL, sig.TTL)
		})
	}

	// Set DF flag (Don't Fragment) - IP_DONTFRAGMENT = 14 on Windows
	if hasQuirk(sig.Quirks, "df") || hasQuirk(sig.Quirks, "df+") {
		_ = raw.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.IPPROTO_IP, 14, 1)
		})
	}

	// Influence window size via SO_RCVBUF
	if sig.WindowType == WindowNormal && sig.WindowSize > 0 {
		_ = raw.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, sig.WindowSize)
		})
	}
}
