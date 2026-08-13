// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package simd_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/simd"
)

func TestMatchWord64(t *testing.T) {
	buf := []byte("Content-Type: application/json")
	assert.True(t, simd.MatchWord64(buf, simd.Word64ContentType))

	buf2 := []byte("Transfer-Encoding: chunked")
	assert.True(t, simd.MatchWord64(buf2, simd.Word64TransferEnc))

	assert.True(t, simd.MatchWord64Str(buf, "Content-Type"))
	assert.False(t, simd.MatchWord64Str(buf, "Invalid-Header"))
}
