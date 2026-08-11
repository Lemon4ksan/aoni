// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package sysnet

// SetTCPMaxSeg is a no-op on Windows as TCP_MAXSEG is not supported.
func SetTCPMaxSeg(_ uintptr, _ int) {}
