// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package arena_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/arena"
)

func TestArenaAllocations(t *testing.T) {
	ar := arena.AcquireArena()
	defer ar.Release()

	b := ar.AllocBytes(128)
	assert.Equal(t, 128, len(b))

	str := ar.AllocString("hello aoni arena")
	assert.Equal(t, "hello aoni arena", str)
}

func TestArenaOverflowFallback(t *testing.T) {
	ar := arena.AcquireArena()
	defer ar.Release()

	// Overflow slab capacity
	big := ar.AllocBytes(arena.DefaultSlabSize + 1024)
	assert.Equal(t, arena.DefaultSlabSize+1024, len(big))
}
