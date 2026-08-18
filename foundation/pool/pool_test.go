// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pool_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/foundation/pool"
)

func TestTimerPool(t *testing.T) {
	t.Parallel()

	timer := pool.AcquireTimer(10 * time.Millisecond)
	assert.NotNil(t, timer)

	<-timer.C

	pool.ReleaseTimer(timer)
}
