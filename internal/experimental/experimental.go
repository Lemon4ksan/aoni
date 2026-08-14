// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package experimental encapsulates extreme hardware acceleration feature flags and platform-specific
// kernel fallbacks (io_uring, CPU affinity pinning, zero-copy DMA, AVX2 SIMD).
package experimental

import (
	"runtime"

	"golang.org/x/sys/cpu"
)

// Features holds hardware capability flags evaluated at runtime.
type Features struct {
	HasAVX2             bool
	HasAVX512           bool
	IsLinuxKernelBypass bool
	IsZeroCopySupported bool
}

// InspectFeatures queries CPU registers and OS runtime capabilities to discover hardware acceleration support.
func InspectFeatures() Features {
	return Features{
		HasAVX2:             cpu.X86.HasAVX2,
		HasAVX512:           cpu.X86.HasAVX512F,
		IsLinuxKernelBypass: runtime.GOOS == "linux",
		IsZeroCopySupported: runtime.GOOS == "linux",
	}
}

// ApplyCPUAffinity locks the calling goroutine's OS thread to designated physical CPU cores.
//
// Concurrency & Runtime Invariant:
// This function calls [runtime.LockOSThread]. The locked OS thread remains bound to the calling
// goroutine for its entire execution duration to prevent OS scheduler thread migration.
// Safe no-op if cores slice is empty or unsupported by the host operating system.
func ApplyCPUAffinity(cores []int) {
	if len(cores) == 0 {
		return
	}

	runtime.LockOSThread()
	setThreadAffinityMask(cores)
}
