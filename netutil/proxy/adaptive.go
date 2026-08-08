// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"time"

	"github.com/lemon4ksan/aoni/internal/mathutil"
	"github.com/lemon4ksan/aoni/telemetry"
)

// AdaptiveTimeoutConfig configures dynamic proxy connection timeouts
// based on observed network round-trip time (RTT).
type AdaptiveTimeoutConfig struct {
	MinTimeout time.Duration
	MaxTimeout time.Duration
	Multiplier float64
}

// DefaultAdaptiveTimeoutConfig returns production defaults for proxy timeout adaptation.
func DefaultAdaptiveTimeoutConfig() AdaptiveTimeoutConfig {
	return AdaptiveTimeoutConfig{
		MinTimeout: 8 * time.Second,
		MaxTimeout: 30 * time.Second,
		Multiplier: 4.0,
	}
}

// ComputeProxyTimeout calculates the dynamic proxy connection timeout based on p95 RTT.
func ComputeProxyTimeout(tracker *telemetry.RTTTracker, cfg AdaptiveTimeoutConfig) time.Duration {
	minT := cfg.MinTimeout
	if minT <= 0 {
		minT = 8 * time.Second
	}

	maxT := cfg.MaxTimeout
	if maxT <= 0 {
		maxT = 30 * time.Second
	}

	mult := cfg.Multiplier
	if mult <= 0 {
		mult = 4.0
	}

	if tracker == nil || tracker.Count() < 3 {
		return 15 * time.Second
	}

	p95 := tracker.P95()
	if p95 <= 0 {
		return 15 * time.Second
	}

	calculated := time.Duration(float64(p95) * mult)

	return max(minT, min(calculated, maxT))
}

// AdaptiveVegasLimiter uses control theory ([mathutil.VegasEngine])
// to dynamically regulate proxy concurrency limits based on RTT latency measurements.
type AdaptiveVegasLimiter struct {
	vegas *mathutil.VegasEngine
}

// NewAdaptiveVegasLimiter constructs an [AdaptiveVegasLimiter].
func NewAdaptiveVegasLimiter(minCwnd, maxCwnd int) *AdaptiveVegasLimiter {
	return &AdaptiveVegasLimiter{
		vegas: mathutil.NewVegasEngine(2.0, 4.0, minCwnd, maxCwnd),
	}
}

// RecordLatency feeds a sample RTT measurement to the control engine.
func (l *AdaptiveVegasLimiter) RecordLatency(sample time.Duration) int {
	if l == nil || l.vegas == nil {
		return 10
	}

	return l.vegas.Update(sample)
}

// CurrentLimit returns the adaptive concurrency limit computed by the Vegas engine.
func (l *AdaptiveVegasLimiter) CurrentLimit() int {
	if l == nil || l.vegas == nil {
		return 10
	}

	return l.vegas.Limit()
}
