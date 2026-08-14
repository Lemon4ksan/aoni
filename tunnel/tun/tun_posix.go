// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux || darwin

package tun

// cStringToGoString converts a null-terminated C-string byte buffer to a Go string.
func cStringToGoString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}

	return string(b)
}
