// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backoff_test

import (
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"

	"github.com/lemon4ksan/aoni/resiliency/backoff"
)

func TestBackoff_Constructors(t *testing.T) {
	t.Parallel()

	// 1. Exponential
	exp := backoff.NewExponential(100*time.Millisecond, 2*time.Second, 2.0)
	assert.NotNil(t, exp)
	d1 := exp.NextDelay(1)
	assert.True(t, d1 >= 100*time.Millisecond)

	// 2. Linear
	lin := backoff.NewLinear(100*time.Millisecond, 1*time.Second, 50*time.Millisecond)
	assert.NotNil(t, lin)
	d2 := lin.NextDelay(2)
	assert.True(t, d2 >= 100*time.Millisecond)

	// 3. Constant
	cons := backoff.NewConstant(250 * time.Millisecond)
	assert.NotNil(t, cons)
	d3 := cons.NextDelay(5)
	assert.Equal(t, 250*time.Millisecond, d3)
}
