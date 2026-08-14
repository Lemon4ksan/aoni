// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package health

import (
	"sync"
	"time"
)

// VegasEngine is a pure control-theory TCP Vegas adaptive concurrency regulator.
// It inspects RTT measurements to dynamically scale concurrency window limits (cwnd).
type VegasEngine struct {
	mu          sync.Mutex
	alpha       float64       // Lower queueing threshold; triggers cwnd expansion when diff < alpha
	beta        float64       // Upper queueing threshold; triggers cwnd reduction when diff > beta
	baseRTT     time.Duration // Minimum observed baseline round-trip time without congestion
	cwnd        float64       // Active floating-point congestion window limit
	minCwnd     int           // Minimum allowed concurrent in-flight requests
	maxCwnd     int           // Maximum allowed concurrent in-flight requests
	sampleCount uint64        // Cumulative number of recorded RTT samples
}

// NewVegasEngine initializes a [VegasEngine] with bounds and thresholds.
func NewVegasEngine(alpha, beta float64, initialCwnd, maxCwnd int) *VegasEngine {
	alphaVal := max(alpha, 1.0)
	betaVal := max(beta, alphaVal+1.0)
	minC := max(initialCwnd, 1)
	maxC := max(maxCwnd, minC)

	return &VegasEngine{
		alpha:   alphaVal,
		beta:    betaVal,
		cwnd:    float64(minC),
		minCwnd: minC,
		maxCwnd: maxC,
	}
}

// Update records a sample RTT and computes the updated concurrency limit.
func (v *VegasEngine) Update(sampleRTT time.Duration) int {
	if sampleRTT <= 0 {
		return v.Limit()
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	v.sampleCount++

	if v.baseRTT == 0 || sampleRTT < v.baseRTT {
		v.baseRTT = sampleRTT
	}

	targetCWND := v.cwnd * (float64(v.baseRTT) / float64(sampleRTT))
	diff := v.cwnd - targetCWND

	if diff < v.alpha {
		v.cwnd += 1.0
	} else if diff > v.beta {
		v.cwnd -= 1.0
	}

	if v.cwnd < float64(v.minCwnd) {
		v.cwnd = float64(v.minCwnd)
	} else if v.cwnd > float64(v.maxCwnd) {
		v.cwnd = float64(v.maxCwnd)
	}

	return int(v.cwnd)
}

// Limit returns the current concurrency window limit.
func (v *VegasEngine) Limit() int {
	v.mu.Lock()
	defer v.mu.Unlock()

	return int(v.cwnd)
}

// BaseRTT returns the minimum observed RTT baseline.
func (v *VegasEngine) BaseRTT() time.Duration {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.baseRTT
}
