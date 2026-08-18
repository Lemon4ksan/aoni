// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package simd_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/foundation/simd"
)

func TestMatchWord64(t *testing.T) {
	buf := []byte("PREFIX_ABC_DATA_TEST")
	targetWord := simd.PackWord64("PREFIX_A")

	assert.True(t, simd.MatchWord64(buf, targetWord))
	assert.True(t, simd.MatchWord64Str(buf, "PREFIX_ABC"))
	assert.False(t, simd.MatchWord64Str(buf, "DIFFERENT_DATA"))
}

func TestMatchWord32(t *testing.T) {
	buf := []byte("ABCD_DATA")
	targetWord := simd.PackWord32("ABCD")

	assert.True(t, simd.MatchWord32(buf, targetWord))
	assert.True(t, simd.MatchWord32Str(buf, "ABCD"))
	assert.False(t, simd.MatchWord32Str(buf, "WXYZ"))
}
