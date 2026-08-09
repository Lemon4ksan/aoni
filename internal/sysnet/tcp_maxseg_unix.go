// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build unix

package sysnet

import "syscall"

// SetTCPMaxSeg sets the TCP_MAXSEG socket option on Linux/Darwin.
func SetTCPMaxSeg(fd uintptr, mss int) {
	syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_MAXSEG, mss) //nolint:errcheck,gosec
}
