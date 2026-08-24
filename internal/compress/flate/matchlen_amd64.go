// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build amd64 && !purego

package flate

import (
	"golang.org/x/sys/cpu"
)

var useAVX2 = cpu.X86.HasAVX2

func matchLen(a, b []byte) (n int) {
	if useAVX2 && len(a) >= 32 && len(b) >= 32 {
		n = matchLenAVX2(a, b)
		if n%32 != 0 || n == len(a) || n == len(b) {
			return n
		}

		a = a[n:]
		b = b[n:]
	}

	return n + matchLenGeneric(a, b)
}

func matchLenAVX2(a, b []byte) int
