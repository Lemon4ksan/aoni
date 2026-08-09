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
	HasAVX2              bool
	HasAVX512            bool
	IsLinuxKernelBypass  bool
	IsZeroCopySupported  bool
	IsHugePagesSupported bool
}

// InspectFeatures queries CPU registers and OS runtime capabilities.
func InspectFeatures() Features {
	return Features{
		HasAVX2:              cpu.X86.HasAVX2,
		HasAVX512:            cpu.X86.HasAVX512F,
		IsLinuxKernelBypass:  runtime.GOOS == "linux",
		IsZeroCopySupported:  runtime.GOOS == "linux",
		IsHugePagesSupported: runtime.GOOS == "windows" || runtime.GOOS == "linux",
	}
}

// ApplyCPUAffinity locks the calling goroutine's OS thread to designated CPU cores.
// Safe no-op if cores slice is empty or unsupported by target OS.
func ApplyCPUAffinity(cores []int) {
	if len(cores) == 0 {
		return
	}

	runtime.LockOSThread()
	setThreadAffinityMask(cores)
}
