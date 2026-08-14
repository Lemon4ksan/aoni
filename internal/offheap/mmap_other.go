// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !windows && !linux && !darwin

package offheap

import (
	"fmt"
	"unsafe"
)

// allocKernelPage falls back to aligned heap memory for other operating systems.
func allocKernelPage(size int) (unsafe.Pointer, error) {
	if size <= 0 {
		return nil, fmt.Errorf("offheap: invalid allocation size %d", size)
	}

	buf := make([]byte, size)

	return unsafe.Pointer(&buf[0]), nil
}

// freeKernelPage is a no-op fallback for non-mmap operating systems.
func freeKernelPage(_ unsafe.Pointer, _ int) error {
	return nil
}
