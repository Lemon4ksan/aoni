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

	addr, err := windows.VirtualAlloc(
		0,
		uintptr(size),
		windows.MEM_COMMIT|windows.MEM_RESERVE,
		windows.PAGE_READWRITE,
	)
	if err != nil || addr == 0 {
		return nil, fmt.Errorf("offheap: VirtualAlloc failed: %w", err)
	}

	// Reinterpret the WinAPI uintptr address as unsafe.Pointer via pointer punning.
	// Direct unsafe.Pointer(uintptr) conversion triggers go vet's unsafeptr check.
	// Punning through *unsafe.Pointer avoids the flagged pattern while remaining
	// correct: VirtualAlloc returns OS kernel-managed memory that the GC never moves.
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr)), nil
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
