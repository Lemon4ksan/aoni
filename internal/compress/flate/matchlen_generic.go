// Copyright (c) 2019-2023 Klaus Post. All rights reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flate

import (
	"math/bits"

	"github.com/lemon4ksan/foundation/silicon/endian"
)

// matchLenGeneric returns the maximum common prefix length of a and b using 64-bit SWAR.
// a must be the shortest of the two.
func matchLenGeneric(a, b []byte) (n int) {
	left := len(a)
	for left >= 32 {
		d0 := endian.Load64(a, n) ^ endian.Load64(b, n)
		if d0 != 0 {
			return n + bits.TrailingZeros64(d0)>>3
		}

		d1 := endian.Load64(a, n+8) ^ endian.Load64(b, n+8)
		if d1 != 0 {
			return n + 8 + bits.TrailingZeros64(d1)>>3
		}

		d2 := endian.Load64(a, n+16) ^ endian.Load64(b, n+16)
		if d2 != 0 {
			return n + 16 + bits.TrailingZeros64(d2)>>3
		}

		d3 := endian.Load64(a, n+24) ^ endian.Load64(b, n+24)
		if d3 != 0 {
			return n + 24 + bits.TrailingZeros64(d3)>>3
		}

		n += 32
		left -= 32
	}

	for left >= 8 {
		diff := endian.Load64(a, n) ^ endian.Load64(b, n)
		if diff != 0 {
			return n + bits.TrailingZeros64(diff)>>3
		}

		n += 8
		left -= 8
	}

	a = a[n:]

	b = b[n:]
	for i := range a {
		if a[i] != b[i] {
			break
		}

		n++
	}

	return n
}

// MatchLen exports matchLen for benchmark and unit testing.
func MatchLen(a, b []byte) int {
	return matchLen(a, b)
}
