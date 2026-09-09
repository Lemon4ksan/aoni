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

// LockGoroutineToCore locks the calling goroutine's OS thread to the designated
// physical CPU cores, preventing OS scheduler thread migration.
//
// This function calls [runtime.LockOSThread] and sets the OS thread CPU affinity mask.
// The lock persists for the lifetime of the calling goroutine.
//
// # Usage Contract
//
// This function MUST be called from the goroutine that will perform the CPU-bound
// or latency-critical work — NOT from library initialization code or constructors.
// Calling it in a constructor locks the initializing goroutine (often main or a
// framework goroutine), which is almost never the intended target.
//
// Example — pin a dedicated I/O goroutine:
//
//	go func() {
//	    sys.LockGoroutineToCore(0, 2) // pin this goroutine's OS thread to cores 0 and 2
//	    for req := range workCh {
//	        process(req)
//	    }
//	}()
//
// Safe no-op if cores slice is empty or affinity is unsupported by the host OS.
func LockGoroutineToCore(cores ...int) {
	if len(cores) == 0 {
		return
	}

	runtime.LockOSThread()
	setThreadAffinityMask(cores)
}

// ApplyCPUAffinity is an alias for [LockGoroutineToCore] kept for backward compatibility.
//
// Deprecated: Use [LockGoroutineToCore] instead. Call it from the goroutine you
// intend to pin, not from initialization code.
func ApplyCPUAffinity(cores []int) {
	LockGoroutineToCore(cores...)
}
