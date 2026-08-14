// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package sysnet

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// writeVectorBuffersFD writes multiple byte slice buffers to fd in a single WSASend vectorized syscall.
func writeVectorBuffersFD(fd uintptr, buffers [][]byte) (int64, error) {
	nBuf := len(buffers)
	if nBuf == 0 {
		return 0, nil
	}

	if nBuf == 1 {
		if len(buffers[0]) == 0 {
			return 0, nil
		}

		var sent uint32

		wsaBuf := windows.WSABuf{
			Len: uint32(len(buffers[0])),
			Buf: &buffers[0][0],
		}

		err := windows.WSASend(windows.Handle(fd), &wsaBuf, 1, &sent, 0, nil, nil)
		if err != nil {
			return int64(sent), err
		}

		return int64(sent), nil
	}

	// Small stack-allocated WSABuf array to prevent heap allocations up to 16 buffers
	var (
		stackBufs [16]windows.WSABuf
		wsaBufs   []windows.WSABuf
	)

	if nBuf <= len(stackBufs) {
		wsaBufs = stackBufs[:nBuf]
	} else {
		wsaBufs = make([]windows.WSABuf, nBuf)
	}

	for i := 0; i < nBuf; i++ {
		if len(buffers[i]) > 0 {
			wsaBufs[i] = windows.WSABuf{
				Len: uint32(len(buffers[i])),
				Buf: (*byte)(unsafe.Pointer(&buffers[i][0])),
			}
		}
	}

	var sent uint32

	err := windows.WSASend(windows.Handle(fd), &wsaBufs[0], uint32(nBuf), &sent, 0, nil, nil)
	if err != nil {
		return int64(sent), err
	}

	return int64(sent), nil
}
