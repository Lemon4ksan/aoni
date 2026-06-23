// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"math"
	"sort"
	"sync"
	"time"
)

// RTTTracker maintains a sliding window of RTT measurements and computes
// percentile-based values (p95, p99) for dynamic hedging delay calculation.
// It is safe for concurrent use.
type RTTTracker struct {
	mu          sync.Mutex
	samples     []time.Duration
	capacity    int
	writeIdx    int
	count       int
	minRTT      time.Duration
	smoothedRTT time.Duration
}

// NewRTTTracker creates an [RTTTracker] with the given sample window capacity.
// A larger capacity provides more stable estimates but reacts slower to changes.
// A capacity of 100 is recommended for most use cases.
func NewRTTTracker(capacity int) *RTTTracker {
	if capacity <= 0 {
		capacity = 100
	}

	return &RTTTracker{
		samples:  make([]time.Duration, capacity),
		capacity: capacity,
	}
}

// Record adds an RTT measurement to the tracker.
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
}

// Percentile returns the given percentile (0-100) of recorded RTT samples.
// Returns 0 if no samples are recorded.
func (t *RTTTracker) Percentile(p float64) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.count == 0 {
		return 0
	}

	sorted := make([]time.Duration, t.count)
	copy(sorted, t.samples[:t.count])
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}

	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}

// P95 returns the 95th percentile RTT.
func (t *RTTTracker) P95() time.Duration {
	return t.Percentile(95)
}

// P99 returns the 99th percentile RTT.
func (t *RTTTracker) P99() time.Duration {
	return t.Percentile(99)
}

// MinRTT returns the minimum observed RTT.
func (t *RTTTracker) MinRTT() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.minRTT
}

// SmoothedRTT returns the exponentially smoothed RTT (EWMA).
func (t *RTTTracker) SmoothedRTT() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.smoothedRTT
}

// Count returns the number of recorded samples.
func (t *RTTTracker) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

// DynamicHedgingConfig configures the dynamic hedging delay calculation.
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

// DefaultDynamicHedgingConfig returns sensible defaults for dynamic hedging.
func DefaultDynamicHedgingConfig() DynamicHedgingConfig {
	return DynamicHedgingConfig{
		Tracker:    NewRTTTracker(100),
		Percentile: 95,
		MinDelay:   50 * time.Millisecond,
		MaxDelay:   2 * time.Second,
		Multiplier: 1.5,
	}
}

// ComputeDelay calculates the dynamic hedging delay based on observed RTT data.
// If the tracker has insufficient samples (< 10), it returns MinDelay.
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

	if delay < minDelay {
		delay = minDelay
	}

	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}
