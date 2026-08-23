// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux

package sys

import "golang.org/x/sys/unix"

func setThreadAffinityMask(cores []int) {
	if len(cores) == 0 {
		return
	}

	var set unix.CPUSet
	for _, core := range cores {
		if core >= 0 && core < len(set)*64 {
			set.Set(core)
		}
	}

	_ = unix.SchedSetaffinity(0, &set)
}
