// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !windows

package arena

// AllocateHugePages allocates HugePages on Linux/Unix or falls back to make([]byte, size).
func AllocateHugePages(size int) []byte {
	if size <= 0 {
		return nil
	}

	return make([]byte, size)
}

// FreeHugePages releases a HugePage slab.
func FreeHugePages(buf []byte) {}
