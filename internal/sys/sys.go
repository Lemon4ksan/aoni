// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sys encapsulates low-level operating system interactions, CPU hardware inspection,
// and platform-specific kernel mechanisms (CPU affinity pinning, zero-copy capabilities, SIMD feature flags).
package sys

import (
	"os"
	"runtime"

	"golang.org/x/sys/cpu"
)

// Features holds hardware capability and kernel support flags evaluated at runtime.
type Features struct {
	HasAVX2             bool
	HasAVX512           bool
	HasARM64NEON        bool
	IsLinuxKernelBypass bool
	IsZeroCopySupported bool
	IsWindowsRIO        bool
	IsLinuxIOUring      bool
	PageSize            int
	NumCPU              int
}

// InspectFeatures queries CPU registers and OS runtime capabilities to discover hardware acceleration support.
func InspectFeatures() Features {
	return Features{
		HasAVX2:             cpu.X86.HasAVX2,
		HasAVX512:           cpu.X86.HasAVX512F,
		HasARM64NEON:        cpu.ARM64.HasASIMD,
		IsLinuxKernelBypass: runtime.GOOS == "linux",
		IsZeroCopySupported: runtime.GOOS == "linux",
		IsWindowsRIO:        runtime.GOOS == "windows",
		IsLinuxIOUring:      runtime.GOOS == "linux",
		PageSize:            os.Getpagesize(),
		NumCPU:              runtime.NumCPU(),
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
