// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build windows

package tcp

// SetTCPMaxSeg is a no-op on Windows as TCP_MAXSEG is not supported.
// Packet fragmentation on Windows is achieved through application-level
// write splitting instead of socket-level MSS control.
func SetTCPMaxSeg(_ uintptr, _ int) {}
