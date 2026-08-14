// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux

package sysnet

import "golang.org/x/sys/unix"

// writeVectorBuffersFD writes multiple byte slice buffers to fd in a single writev vectorized syscall.
func writeVectorBuffersFD(fd uintptr, buffers [][]byte) (int64, error) {
	nBuf := len(buffers)
	if nBuf == 0 {
		return 0, nil
	}

	if nBuf == 1 {
		if len(buffers[0]) == 0 {
			return 0, nil
		}

		n, err := unix.Write(int(fd), buffers[0])

		return int64(n), err
	}

	n, err := unix.Writev(int(fd), buffers)
	if err != nil {
		return int64(n), err
	}

	return int64(n), nil
}
