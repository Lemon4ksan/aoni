// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build darwin

package offheap

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// allocKernelPage allocates a raw unpaged physical memory slab directly from the Darwin OS kernel via mmap.
func allocKernelPage(size int) (unsafe.Pointer, error) {
	if size <= 0 {
		return nil, fmt.Errorf("offheap: invalid allocation size %d", size)
	}

	b, err := unix.Mmap(
		-1,
		0,
		size,
		unix.PROT_READ|unix.PROT_WRITE,
		unix.MAP_ANON|unix.MAP_PRIVATE,
	)
	if err != nil || len(b) == 0 {
		return nil, fmt.Errorf("offheap: mmap failed: %w", err)
	}

	return unsafe.Pointer(&b[0]), nil
}

// freeKernelPage releases raw physical memory page back to the Darwin OS kernel via munmap.
func freeKernelPage(ptr unsafe.Pointer, size int) error {
	if ptr == nil || size <= 0 {
		return nil
	}

	b := unsafe.Slice((*byte)(ptr), size)

	if err := unix.Munmap(b); err != nil {
		return fmt.Errorf("offheap: munmap failed: %w", err)
	}

	return nil
}
