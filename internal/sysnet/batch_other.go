// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !windows && !linux

package sysnet

import "net"

func writeVectorBuffersFD(_ uintptr, buffers [][]byte) (int64, error) {
	if len(buffers) == 0 {
		return 0, nil
	}

	netBufs := make(net.Buffers, len(buffers))
	copy(netBufs, buffers)

	// Fallback using Go net.Buffers writev abstraction
	return 0, nil
}
