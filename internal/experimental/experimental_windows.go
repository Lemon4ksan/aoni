// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package experimental

import (
	"syscall"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentThread      = kernel32.NewProc("GetCurrentThread")
	procSetThreadAffinityMask = kernel32.NewProc("SetThreadAffinityMask")
)

func setThreadAffinityMask(cores []int) {
	if len(cores) == 0 {
		return
	}

	var mask uintptr
	for _, core := range cores {
		if core >= 0 && core < 64 {
			mask |= (uintptr(1) << core)
		}
	}

	if mask == 0 {
		return
	}

	threadHandle, _, _ := procGetCurrentThread.Call()
	if threadHandle != 0 {
		_, _, _ = procSetThreadAffinityMask.Call(threadHandle, mask)
	}
}
