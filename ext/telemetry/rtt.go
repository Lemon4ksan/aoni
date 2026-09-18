// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package telemetry

import (
	"time"

	"github.com/lemon4ksan/foundation/silicon/metrics"
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
// Core implementation is located in [github.com/lemon4ksan/foundation/silicon/metrics].
type RTTTracker = metrics.RTTTracker

// NewRTTTracker instantiates an [RTTTracker] with sample window capacity.
func NewRTTTracker(capacity int) *RTTTracker {
	return metrics.NewRTTTracker(capacity)
}
