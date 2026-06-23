// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux || darwin

package aoni

import "syscall"

// setTCPMaxSeg sets the TCP_MAXSEG socket option on Linux/Darwin.
func setTCPMaxSeg(fd uintptr, mss int) {
	syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_MAXSEG, mss) //nolint:errcheck
}
