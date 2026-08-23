// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRandomNumbers(t *testing.T) {
	const (
		num = 1000
		max = 12345678
	)

	var (
		values [num]int32
		r      Rand
	)

	for i := range num {
		v := r.Int31n(max)
		require.GreaterOrEqual(t, v, int32(0))
		require.Less(t, v, int32(max))
		values[i] = v
	}

	var sum uint64
	for _, n := range values {
		sum += uint64(n)
	}

	average := float64(sum) / num
	expectedAverage := float64(max) / 2
	tolerance := float64(max) / 25
	require.InDelta(t, expectedAverage, average, tolerance)
}
