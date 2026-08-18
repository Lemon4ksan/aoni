// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package simd_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/foundation/simd"
)

func TestBMI2BitManipulation(t *testing.T) {
	val := uint64(0b10101010)
	mask := uint64(0b11110000)

	ext := simd.ExtractBits(val, mask)
	assert.Equal(t, uint64(0b1010), ext)

	dep := simd.DepositBits(ext, mask)
	assert.Equal(t, uint64(0b10100000), dep)

	tz := simd.CountTrailingZeros(0b1000)
	assert.Equal(t, 3, tz)
}
