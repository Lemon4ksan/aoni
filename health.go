// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"sync/atomic"
	"time"
)

// HealthTracker tracks the health state of a single endpoint (proxy or backend)
// using consecutive failure counting with automatic recovery after a cooldown.
type HealthTracker struct {
	failCount   atomic.Uint32
	unhealthy   atomic.Bool
	recoveredAt atomic.Int64

	maxFails    uint32
	retryAfter  time.Duration
	name        string
	onUnhealthy func(name string, fails uint32, retryAfter time.Duration)
	onRecovered func(name string)
}

// NewHealthTracker creates a [HealthTracker] with the given parameters.
// onUnhealthy and onRecovered are called when the state changes; they may be nil.
func NewHealthTracker(
	name string,
	maxFails uint32,
	retryAfter time.Duration,
	onUnhealthy func(string, uint32, time.Duration),
	onRecovered func(string),
) *HealthTracker {
	return &HealthTracker{
		maxFails:    maxFails,
		retryAfter:  retryAfter,
		name:        name,
		onUnhealthy: onUnhealthy,
		onRecovered: onRecovered,
	}
}

// MarkFailed records a failure. When consecutive failures reach maxFails,
// the endpoint is marked unhealthy until the recovery time elapses.
func (h *HealthTracker) MarkFailed() {
	fails := h.failCount.Add(1)
	if fails >= h.maxFails {
		h.unhealthy.Store(true)
		h.recoveredAt.Store(time.Now().Add(h.retryAfter).UnixNano())

		if h.onUnhealthy != nil {
			h.onUnhealthy(h.name, fails, h.retryAfter)
		}
	}
}

// MarkSuccess resets the failure counter and marks the endpoint as healthy.
func (h *HealthTracker) MarkSuccess() {
	wasUnhealthy := h.unhealthy.Load()
	h.failCount.Store(0)
	h.unhealthy.Store(false)

	if wasUnhealthy && h.onRecovered != nil {
		h.onRecovered(h.name)
	}
}

// IsAvailable reports whether the endpoint is reachable.
// An unhealthy endpoint becomes available again after its recovery time elapses.
func (h *HealthTracker) IsAvailable() bool {
	if !h.unhealthy.Load() {
		return true
	}

	if time.Now().UnixNano() >= h.recoveredAt.Load() {
		return true
	}

	return false
}
