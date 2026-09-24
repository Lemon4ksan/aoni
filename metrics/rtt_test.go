// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metrics_test

import (
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testing/assert"

	"github.com/lemon4ksan/aoni/metrics"
)

func TestRTTTracker_SlidingWindow_And_Percentiles(t *testing.T) {
	t.Parallel()

	tracker := metrics.NewRTTTracker(5)

	tracker.Record(10 * time.Millisecond)
	tracker.Record(20 * time.Millisecond)
	tracker.Record(30 * time.Millisecond)
	tracker.Record(40 * time.Millisecond)
	tracker.Record(50 * time.Millisecond)

	assert.Equal(t, 5, tracker.Count())
	assert.Equal(t, 10*time.Millisecond, tracker.MinRTT())
	assert.Equal(t, 50*time.Millisecond, tracker.MaxRTT())
	assert.Equal(t, 30*time.Millisecond, tracker.AverageRTT())
	assert.Equal(t, 50*time.Millisecond, tracker.P95())
	assert.Equal(t, 50*time.Millisecond, tracker.P99())
	assert.Equal(t, 30*time.Millisecond, tracker.Percentile(50))

	// Sliding window overflow: pushes out oldest 10ms, adds 60ms
	tracker.Record(60 * time.Millisecond)
	assert.Equal(t, 5, tracker.Count())
	assert.Equal(t, 60*time.Millisecond, tracker.MaxRTT())
	assert.Equal(t, 40*time.Millisecond, tracker.AverageRTT())

	tracker.Reset()
	assert.Equal(t, 0, tracker.Count())
	assert.Equal(t, time.Duration(0), tracker.MinRTT())
	assert.Equal(t, time.Duration(0), tracker.MaxRTT())
}
