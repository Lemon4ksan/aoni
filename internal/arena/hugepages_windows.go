// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package arena

import (
	"syscall"
	"unsafe"
)

const (
	MEM_COMMIT      = 0x1000
	MEM_RESERVE     = 0x2000
	MEM_RELEASE     = 0x8000
	MEM_LARGE_PAGES = 0x20000000
	PAGE_READWRITE  = 0x04
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procVirtualAlloc = kernel32.NewProc("VirtualAlloc")
	procVirtualFree  = kernel32.NewProc("VirtualFree")
)

// AllocateHugePages allocates a 2 MB contiguous HugePage slab via VirtualAlloc (MEM_LARGE_PAGES).
// If SeLockMemoryPrivilege is missing, gracefully falls back to standard VirtualAlloc.
func AllocateHugePages(size int) []byte {
	if size <= 0 {
		return nil
	}

	// Round up to 2 MB (2048 KB) LargePage boundary
	const hugePage2MB = 2 * 1024 * 1024

	allocSize := (size + hugePage2MB - 1) &^ (hugePage2MB - 1)

	// Try VirtualAlloc with MEM_LARGE_PAGES
	addr, _, _ := procVirtualAlloc.Call(
		0,
		uintptr(allocSize),
		uintptr(MEM_COMMIT|MEM_RESERVE|MEM_LARGE_PAGES),
		uintptr(PAGE_READWRITE),
	)

	if addr == 0 {
		// Fallback to standard VirtualAlloc if LargePage privilege is missing
		addr, _, _ = procVirtualAlloc.Call(
			0,
			uintptr(allocSize),
			uintptr(MEM_COMMIT|MEM_RESERVE),
			uintptr(PAGE_READWRITE),
		)
	}

	if addr == 0 {
		return make([]byte, size)
	}

	return unsafe.Slice((*byte)(unsafe.Pointer(addr)), allocSize) //nolint:govet
}

// FreeHugePages releases a HugePage slab back to the Windows kernel.
func FreeHugePages(buf []byte) {
	if len(buf) == 0 {
		return
	}

	addr := uintptr(unsafe.Pointer(&buf[0]))
	_, _, _ = procVirtualFree.Call(addr, 0, uintptr(MEM_RELEASE))
}
