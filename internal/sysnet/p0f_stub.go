// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !linux && !darwin && !freebsd && !openbsd && !windows

package sysnet

import "syscall"

// ApplyP0fSignature is a no-op fallback for unsupported operating systems.
func ApplyP0fSignature(raw syscall.RawConn, ttl, windowSize int, setWindow, hasDF bool) {}
