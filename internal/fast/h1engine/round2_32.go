// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !amd64 && !arm64 && !ppc64 && !ppc64le && !riscv64 && !s390x

package h1engine

import "math"

func roundUpForSliceCap(n int) int {
	if n <= 0 {
		return 0
	}

	// Above 100MB, we don't round up as the overhead is too large.
	if n > 100*1024*1024 {
		return n
	}

	x := uint32(n - 1)
	x |= x >> 1
	x |= x >> 2
	x |= x >> 4
	x |= x >> 8
	x |= x >> 16

	// Make sure we don't return 0 due to overflow, even on 32 bit systems
	if x >= uint32(math.MaxInt32) {
		return math.MaxInt32
	}

	return int(x + 1)
}
