// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package offheap

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// allocKernelPage Memory allocates a raw unpaged physical memory slab directly from OS kernel.
func allocKernelPage(size int) (unsafe.Pointer, error) {
	if size <= 0 {
		return nil, fmt.Errorf("offheap: invalid allocation size %d", size)
	}

	ptr, err := windows.VirtualAlloc(
		0,
		uintptr(size),
		windows.MEM_COMMIT|windows.MEM_RESERVE,
		windows.PAGE_READWRITE,
	)
	if err != nil || ptr == 0 {
		return nil, fmt.Errorf("offheap: VirtualAlloc failed: %w", err)
	}

	return unsafe.Pointer(ptr), nil
}

// freeKernelPage releases raw physical memory page back to OS kernel.
func freeKernelPage(ptr unsafe.Pointer, _ int) error {
	if ptr == nil {
		return nil
	}

	err := windows.VirtualFree(uintptr(ptr), 0, windows.MEM_RELEASE)
	if err != nil {
		return fmt.Errorf("offheap: VirtualFree failed: %w", err)
	}

	return nil
}
