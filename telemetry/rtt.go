// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package telemetry

import (
	"math"
	"slices"
	"sync"
	"time"
)

// DynamicHedgingConfig configures percentile-based dynamic request hedging.
type DynamicHedgingConfig struct {
	// Tracker is the shared RTT tracker for measuring network latency.
	Tracker *RTTTracker

	// Percentile is the RTT percentile to use for delay calculation (default: 95).
	Percentile float64

	// MinDelay is the minimum hedging delay regardless of RTT (default: 50ms).
	MinDelay time.Duration

	// MaxDelay is the maximum hedging delay cap (default: 2s).
	MaxDelay time.Duration

	// Multiplier scales the percentile RTT to compute the delay (default: 1.5).
	// The dynamic delay = min(MaxDelay, max(MinDelay, p95 * Multiplier)).
	Multiplier float64
}

// DefaultDynamicHedgingConfig returns production defaults for dynamic request hedging.
func DefaultDynamicHedgingConfig() DynamicHedgingConfig {
	return DynamicHedgingConfig{
		Tracker:    NewRTTTracker(100),
		Percentile: 95,
		MinDelay:   50 * time.Millisecond,
		MaxDelay:   2 * time.Second,
		Multiplier: 1.5,
	}
}

// ComputeDelay calculates dynamic hedging delay based on observed RTT percentile values.
func (c DynamicHedgingConfig) ComputeDelay() time.Duration {
	if c.Tracker == nil || c.Tracker.Count() < 10 {
		if c.MinDelay > 0 {
			return c.MinDelay
		}

		return 50 * time.Millisecond
	}

	pct := c.Percentile
	if pct <= 0 {
		pct = 95
	}

	rtt := c.Tracker.Percentile(pct)

	mult := c.Multiplier
	if mult <= 0 {
		mult = 1.5
	}

	delay := time.Duration(float64(rtt) * mult)

	minDelay := c.MinDelay
	if minDelay <= 0 {
		minDelay = 50 * time.Millisecond
	}

	maxDelay := c.MaxDelay
	if maxDelay <= 0 {
		maxDelay = 2 * time.Second
	}

	return max(minDelay, min(delay, maxDelay))
}

// RTTTracker maintains a sliding window of network round-trip time measurements.
type RTTTracker struct {
	mu          sync.Mutex
	samples     []time.Duration
	capacity    int
	writeIdx    int
	count       int
	minRTT      time.Duration
	smoothedRTT time.Duration

	dirty        bool
	cachedSorted []time.Duration
}

// NewRTTTracker instantiates an [RTTTracker] with sample window capacity.
func NewRTTTracker(capacity int) *RTTTracker {
	if capacity <= 0 {
		capacity = 100
	}

	return &RTTTracker{
		samples:  make([]time.Duration, capacity),
		capacity: capacity,
	}
}

// Record registers an RTT measurement sample.
func (t *RTTTracker) Record(rtt time.Duration) {
	if rtt <= 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.samples[t.writeIdx] = rtt
	t.writeIdx = (t.writeIdx + 1) % t.capacity

	if t.count < t.capacity {
		t.count++
	}

	if t.minRTT == 0 || rtt < t.minRTT {
		t.minRTT = rtt
	}

	if t.smoothedRTT == 0 {
		t.smoothedRTT = rtt
	} else {
		t.smoothedRTT = time.Duration(0.9*float64(t.smoothedRTT) + 0.1*float64(rtt))
	}

	t.dirty = true
}

// Percentile evaluates the specified percentile (0-100) across recorded RTT samples.
func (t *RTTTracker) Percentile(p float64) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.count == 0 {
		return 0
	}

	if t.dirty || len(t.cachedSorted) != t.count {
		if len(t.cachedSorted) != t.count {
			t.cachedSorted = make([]time.Duration, t.count)
		}

		copy(t.cachedSorted, t.samples[:t.count])
		slices.Sort(t.cachedSorted)
		t.dirty = false
	}

	idx := max(int(math.Ceil(p/100*float64(len(t.cachedSorted))))-1, 0)
	if idx >= len(t.cachedSorted) {
		idx = len(t.cachedSorted) - 1
	}

	return t.cachedSorted[idx]
}

// P95 returns 95th percentile RTT.
func (t *RTTTracker) P95() time.Duration {
	return t.Percentile(95)
}

// P99 returns 99th percentile RTT.
func (t *RTTTracker) P99() time.Duration {
	return t.Percentile(99)
}

// MinRTT returns minimum observed RTT.
func (t *RTTTracker) MinRTT() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.minRTT
}

// MaxRTT returns maximum observed RTT within the current sliding window.
func (t *RTTTracker) MaxRTT() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.count == 0 {
		return 0
	}

	maxVal := t.samples[0]
	for i := 1; i < t.count; i++ {
		if t.samples[i] > maxVal {
			maxVal = t.samples[i]
		}
	}

	return maxVal
}

// AverageRTT computes the arithmetic mean across recorded RTT samples.
func (t *RTTTracker) AverageRTT() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.count == 0 {
		return 0
	}

	var sum int64
	for i := range t.count {
		sum += int64(t.samples[i])
	}

	return time.Duration(sum / int64(t.count))
}

// SmoothedRTT returns exponentially weighted moving average (EWMA) RTT.
func (t *RTTTracker) SmoothedRTT() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.smoothedRTT
}

// Count returns recorded sample count.
func (t *RTTTracker) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.count
}

// Reset clears recorded RTT samples and resets tracking state.
func (t *RTTTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.writeIdx = 0
	t.count = 0
	t.minRTT = 0
	t.smoothedRTT = 0
	t.dirty = true
	t.cachedSorted = nil

	clear(t.samples)
}
