// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux

package p0f

import (
	"syscall"
	"unsafe"
)

func setDF(fd uintptr, enable bool) error {
	val := syscall.IP_PMTUDISC_DO
	if !enable {
		val = syscall.IP_PMTUDISC_WANT
	}

	_, _, errno := syscall.Syscall6(
		syscall.SYS_SETSOCKOPT,
		fd,
		uintptr(syscall.IPPROTO_IP),
		uintptr(syscall.IP_MTU_DISCOVER),
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
		_ = raw.Control(func(fd uintptr) {
			ttl := sig.TTL
			_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_TTL, ttl) //nolint:gosec
		})
	}

	// Set DF flag (Don't Fragment)
	if hasQuirk(sig.Quirks, "df") || hasQuirk(sig.Quirks, "df+") {
		_ = raw.Control(func(fd uintptr) {
			_ = setDF(fd, true)
		})
	}

	// Influence window size via SO_RCVBUF
	if sig.WindowType == WindowNormal && sig.WindowSize > 0 {
		_ = raw.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, sig.WindowSize) //nolint:gosec
		})
	}
}
