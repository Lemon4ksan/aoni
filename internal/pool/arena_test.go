// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pool_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/pool"
)

func TestRequestArena_AllocAndReset(t *testing.T) {
	arena := pool.GetRequestArena()
	defer pool.ReleaseRequestArena(arena)

	b1 := arena.Alloc(128)
	assert.Equal(t, 128, len(b1))

	b2 := arena.Alloc(256)
	assert.Equal(t, 256, len(b2))

	arena.Reset()
	b3 := arena.Alloc(64)
	assert.Equal(t, 64, len(b3))
}
