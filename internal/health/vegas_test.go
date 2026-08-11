// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package health_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/health"
)

func TestVegasEngine(t *testing.T) {
	t.Parallel()

	engine := health.NewVegasEngine(2.0, 4.0, 10, 100)
	assert.Equal(t, 10, engine.Limit())

	engine.Update(10 * time.Millisecond)
	assert.Equal(t, 10*time.Millisecond, engine.BaseRTT())

	newLimit := engine.Update(10 * time.Millisecond)
	assert.GreaterOrEqual(t, newLimit, 10)

	for range 10 {
		engine.Update(50 * time.Millisecond)
	}

	assert.LessOrEqual(t, engine.Limit(), 100)
}
