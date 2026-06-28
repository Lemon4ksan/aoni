// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRTTTracker_CapacityFallback(t *testing.T) {
	t.Parallel()

	t.Run("valid_capacity", func(t *testing.T) {
		t.Parallel()

		tracker := NewRTTTracker(10)
		assert.Equal(t, 10, tracker.capacity)
	})

	t.Run("invalid_capacity_fallback", func(t *testing.T) {
		t.Parallel()

		tracker := NewRTTTracker(-5)
		assert.Equal(t, 100, tracker.capacity)
	})
}

func TestRTTTracker_Record_And_Averages(t *testing.T) {
	t.Parallel()

	tracker := NewRTTTracker(5)

	// 1. Ignoring non-positive RTT values
	tracker.Record(-10 * time.Millisecond)
	tracker.Record(0)
	assert.Equal(t, 0, tracker.Count())

	// 2. Add first valid sample
	tracker.Record(10 * time.Millisecond)
	assert.Equal(t, 1, tracker.Count())
	assert.Equal(t, 10*time.Millisecond, tracker.MinRTT())
	assert.Equal(t, 10*time.Millisecond, tracker.MaxRTT())
	assert.Equal(t, 10*time.Millisecond, tracker.SmoothedRTT())
	assert.Equal(t, 10*time.Millisecond, tracker.AverageRTT())

	// 3. Add second sample
	tracker.Record(20 * time.Millisecond)
	assert.Equal(t, 2, tracker.Count())
	assert.Equal(t, 10*time.Millisecond, tracker.MinRTT())
	assert.Equal(t, 20*time.Millisecond, tracker.MaxRTT())
	// EWMA: 0.9 * 10ms + 0.1 * 20ms = 11ms
	assert.Equal(t, 11*time.Millisecond, tracker.SmoothedRTT())
	assert.Equal(t, 15*time.Millisecond, tracker.AverageRTT()) // (10 + 20) / 2 = 15

	// 4. Fill to capacity and verify wrap-around
	tracker.Record(30 * time.Millisecond)
	tracker.Record(40 * time.Millisecond)
	tracker.Record(50 * time.Millisecond)
	assert.Equal(t, 5, tracker.Count())

	// Overflow
	tracker.Record(5 * time.Millisecond) // replaces the first sample (10ms)
	assert.Equal(t, 5, tracker.Count())
	assert.Equal(t, 5*time.Millisecond, tracker.MinRTT()) // min RTT updated
	assert.Equal(t, 50*time.Millisecond, tracker.MaxRTT())

	// Samples should be [5, 20, 30, 40, 50]ms (sum = 145ms)
	assert.Equal(t, 29*time.Millisecond, tracker.AverageRTT()) // 145 / 5 = 29
}

func TestRTTTracker_Percentiles(t *testing.T) {
	t.Parallel()

	tracker := NewRTTTracker(10)

	// Percentiles on empty tracker should return 0
	assert.Equal(t, time.Duration(0), tracker.Percentile(95))

	// Fill with distinct values
	for i := 1; i <= 10; i++ {
		tracker.Record(time.Duration(i) * time.Millisecond) // [1..10]ms
	}

	assert.Equal(t, 1*time.Millisecond, tracker.Percentile(0))
	assert.Equal(t, 5*time.Millisecond, tracker.Percentile(50))
	assert.Equal(t, 10*time.Millisecond, tracker.P95())
	assert.Equal(t, 10*time.Millisecond, tracker.P99())
	assert.Equal(t, 10*time.Millisecond, tracker.Percentile(100))
}

func TestRTTTracker_Reset(t *testing.T) {
	t.Parallel()

	tracker := NewRTTTracker(10)
	tracker.Record(50 * time.Millisecond)
	tracker.Record(100 * time.Millisecond)

	assert.Equal(t, 2, tracker.Count())
	assert.Equal(t, 50*time.Millisecond, tracker.MinRTT())
	assert.Equal(t, 100*time.Millisecond, tracker.MaxRTT())

	// Reset
	tracker.Reset()
	assert.Equal(t, 0, tracker.Count())
	assert.Equal(t, time.Duration(0), tracker.MinRTT())
	assert.Equal(t, time.Duration(0), tracker.MaxRTT())
	assert.Equal(t, time.Duration(0), tracker.AverageRTT())
}

func TestDynamicHedgingConfig_ComputeDelay(t *testing.T) {
	t.Parallel()

	t.Run("nil_tracker_or_low_samples", func(t *testing.T) {
		t.Parallel()

		// Nil tracker -> returns MinDelay or fallback 50ms
		cfgNil := DynamicHedgingConfig{Tracker: nil, MinDelay: 30 * time.Millisecond}
		assert.Equal(t, 30*time.Millisecond, cfgNil.ComputeDelay())

		cfgNilFallback := DynamicHedgingConfig{Tracker: nil, MinDelay: 0}
		assert.Equal(t, 50*time.Millisecond, cfgNilFallback.ComputeDelay())

		// Low samples (< 10) -> returns MinDelay or fallback 50ms
		tracker := NewRTTTracker(20)
		tracker.Record(100 * time.Millisecond)
		cfgLow := DynamicHedgingConfig{Tracker: tracker, MinDelay: 40 * time.Millisecond}
		assert.Equal(t, 40*time.Millisecond, cfgLow.ComputeDelay())
	})

	t.Run("valid_samples_calculates_delay", func(t *testing.T) {
		t.Parallel()

		tracker := NewRTTTracker(20)
		// Fill with 10 samples (100ms each)
		for range 10 {
			tracker.Record(100 * time.Millisecond)
		}

		// P95 is 100ms. Delay = P95 * Multiplier = 100ms * 1.5 = 150ms
		cfg := DynamicHedgingConfig{
			Tracker:    tracker,
			Percentile: 95,
			MinDelay:   50 * time.Millisecond,
			MaxDelay:   2 * time.Second,
			Multiplier: 1.5,
		}
		assert.Equal(t, 150*time.Millisecond, cfg.ComputeDelay())
	})

	t.Run("default_fallbacks_and_boundaries", func(t *testing.T) {
		t.Parallel()

		tracker := NewRTTTracker(20)
		for range 10 {
			tracker.Record(100 * time.Millisecond)
		}

		// Zero fields should apply fallbacks (Percentile: 95, Multiplier: 1.5, MinDelay: 50ms, MaxDelay: 2s)
		cfgZero := DynamicHedgingConfig{Tracker: tracker}
		assert.Equal(t, 150*time.Millisecond, cfgZero.ComputeDelay())

		// Result hits MinDelay constraint
		cfgMinCap := DynamicHedgingConfig{
			Tracker:    tracker,
			Multiplier: 0.1, // 100ms * 0.1 = 10ms
			MinDelay:   80 * time.Millisecond,
		}
		assert.Equal(t, 80*time.Millisecond, cfgMinCap.ComputeDelay())

		// Result hits MaxDelay constraint
		cfgMaxCap := DynamicHedgingConfig{
			Tracker:    tracker,
			Multiplier: 10.0, // 100ms * 10 = 1000ms
			MaxDelay:   300 * time.Millisecond,
		}
		assert.Equal(t, 300*time.Millisecond, cfgMaxCap.ComputeDelay())
	})
}

func TestRTTTracker_Concurrency(t *testing.T) {
	t.Parallel()

	tracker := NewRTTTracker(50)

	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(1)

		go func(val int) {
			defer wg.Done()

			tracker.Record(time.Duration(val) * time.Millisecond)
			_ = tracker.P95()
			_ = tracker.SmoothedRTT()
			_ = tracker.AverageRTT()
		}(i)
	}

	wg.Wait()
	assert.Equal(t, 50, tracker.Count())
}
