// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package simd

import "math/bits"

// ExtractBits extracts contiguous bits from val according to mask (BMI2 PEXT hardware or SWAR fallback).
func ExtractBits(val, mask uint64) uint64 {
	return extractBitsHW(val, mask)
}

// DepositBits deposits contiguous bits from val into mask locations (BMI2 PDEP hardware or SWAR fallback).
func DepositBits(val, mask uint64) uint64 {
	return depositBitsHW(val, mask)
}

// CountTrailingZeros returns the number of trailing zero bits in x (TZCNT).
func CountTrailingZeros(x uint64) int {
	return bits.TrailingZeros64(x)
}

// CountLeadingZeros returns the number of leading zero bits in x (LZCNT).
func CountLeadingZeros(x uint64) int {
	return bits.LeadingZeros64(x)
}

// extractBitsSWAR emulates BMI2 PEXT in software using SIMD-Within-A-Register bit operations.
func extractBitsSWAR(val, mask uint64) uint64 {
	res := uint64(0)
	outBit := uint64(0)

	for i := 0; i < 64; i++ {
		if (mask & (1 << i)) != 0 {
			if (val & (1 << i)) != 0 {
				res |= (1 << outBit)
			}

			outBit++
		}
	}

	return res
}

// depositBitsSWAR emulates BMI2 PDEP in software using SIMD-Within-A-Register bit operations.
func depositBitsSWAR(val, mask uint64) uint64 {
	res := uint64(0)
	inBit := uint64(0)

	for i := 0; i < 64; i++ {
		if (mask & (1 << i)) != 0 {
			if (val & (1 << inBit)) != 0 {
				res |= (1 << i)
			}

			inBit++
		}
	}

	return res
}
