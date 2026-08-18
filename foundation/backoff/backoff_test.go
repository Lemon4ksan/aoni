// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backoff_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/lemon4ksan/aoni/foundation/backoff"
)

func TestExponentialBackoff_Deterministic(t *testing.T) {
	t.Parallel()

	bo := backoff.NewExponential(100*time.Millisecond, 1*time.Second, 2.0)

	assert.Equal(t, 100*time.Millisecond, bo.NextDelay(1))
	assert.Equal(t, 200*time.Millisecond, bo.NextDelay(2))
	assert.Equal(t, 400*time.Millisecond, bo.NextDelay(3))
	assert.Equal(t, 800*time.Millisecond, bo.NextDelay(4))
	assert.Equal(t, 1000*time.Millisecond, bo.NextDelay(5)) // capped at max
	assert.Equal(t, 1000*time.Millisecond, bo.NextDelay(10))
}

func TestExponentialBackoff_FullJitter(t *testing.T) {
	t.Parallel()

	bo := backoff.NewExponential(100*time.Millisecond, 1*time.Second, 2.0).WithFullJitter()

	for attempt := 1; attempt <= 5; attempt++ {
		delay := bo.NextDelay(attempt)
		assert.GreaterOrEqual(t, delay, time.Duration(0))
		assert.LessOrEqual(t, delay, 1*time.Second)
	}
}

func TestExponentialBackoff_EqualJitter(t *testing.T) {
	t.Parallel()

	bo := backoff.NewExponential(100*time.Millisecond, 1*time.Second, 2.0).WithEqualJitter()

	for attempt := 1; attempt <= 5; attempt++ {
		delay := bo.NextDelay(attempt)
		assert.GreaterOrEqual(t, delay, 50*time.Millisecond)
		assert.LessOrEqual(t, delay, 1*time.Second)
	}
}

func TestExponentialBackoff_DecorrelatedJitter(t *testing.T) {
	t.Parallel()

	bo := backoff.NewExponential(50*time.Millisecond, 500*time.Millisecond, 2.0).WithDecorrelatedJitter()

	for attempt := 1; attempt <= 10; attempt++ {
		delay := bo.NextDelay(attempt)
		assert.GreaterOrEqual(t, delay, 50*time.Millisecond)
		assert.LessOrEqual(t, delay, 500*time.Millisecond)
	}

	bo.Reset()
}

func TestLinearBackoff(t *testing.T) {
	t.Parallel()

	bo := backoff.NewLinear(100*time.Millisecond, 500*time.Millisecond, 100*time.Millisecond)

	assert.Equal(t, 100*time.Millisecond, bo.NextDelay(1))
	assert.Equal(t, 200*time.Millisecond, bo.NextDelay(2))
	assert.Equal(t, 300*time.Millisecond, bo.NextDelay(3))
	assert.Equal(t, 400*time.Millisecond, bo.NextDelay(4))
	assert.Equal(t, 500*time.Millisecond, bo.NextDelay(5))
	assert.Equal(t, 500*time.Millisecond, bo.NextDelay(6)) // capped at max
}

func TestConstantBackoff(t *testing.T) {
	t.Parallel()

	bo := backoff.NewConstant(250 * time.Millisecond)

	assert.Equal(t, 250*time.Millisecond, bo.NextDelay(1))
	assert.Equal(t, 250*time.Millisecond, bo.NextDelay(5))
}

func BenchmarkExponentialBackoff_ZeroAlloc(b *testing.B) {
	bo := backoff.NewExponential(100*time.Millisecond, 10*time.Second, 2.0).WithFullJitter()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = bo.NextDelay((i % 10) + 1)
	}
}
