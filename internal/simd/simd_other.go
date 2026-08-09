// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !amd64 || purego

package simd

// IndexByteVector falls back to SWAR on non-amd64 architectures.
func IndexByteVector(b []byte, c byte) int {
	return IndexByteSWAR(b, c)
}
