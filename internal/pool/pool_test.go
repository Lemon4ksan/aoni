// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pool_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/internal/pool"
)

func TestHeaderPool(t *testing.T) {
	t.Parallel()

	h := pool.AcquireHeader()
	assert.NotNil(t, h)

	h.Set("X-Test", "value")
	assert.Equal(t, "value", h.Get("X-Test"))

	pool.ReleaseHeader(h)

	h2 := pool.AcquireHeader()
	assert.Empty(t, h2.Get("X-Test"))

	pool.ReleaseHeader(h2)
}

func TestTimerPool(t *testing.T) {
	t.Parallel()

	timer := pool.AcquireTimer(10 * time.Millisecond)
	assert.NotNil(t, timer)

	<-timer.C

	pool.ReleaseTimer(timer)
}
