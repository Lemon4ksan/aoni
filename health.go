// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"sync/atomic"
	"time"
)

// HealthStatus represents the detailed state of a tracked endpoint.
type HealthStatus int

const (
	// StatusHealthy indicates the endpoint is fully functional with zero consecutive failures.
	StatusHealthy HealthStatus = iota
	// StatusDegraded indicates the endpoint is still functional but has experienced some failures.
	StatusDegraded
	// StatusUnhealthy indicates the endpoint has exceeded max failures and is currently in cooldown.
	StatusUnhealthy
	// StatusRecovering indicates the cooldown has elapsed and the endpoint is ready for a trial request.
	StatusRecovering
)

// String returns a human-readable representation of the HealthStatus.
func (s HealthStatus) String() string {
	switch s {
	case StatusHealthy:
		return "Healthy"
	case StatusDegraded:
		return "Degraded"
	case StatusUnhealthy:
		return "Unhealthy"
	case StatusRecovering:
		return "Recovering"
	default:
		return "Unknown"
	}
}

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

// FailCount returns the current number of consecutive failures.
func (h *HealthTracker) FailCount() uint32 {
	return h.failCount.Load()
}

// CooldownRemaining returns the time remaining in the cooldown phase.
// Returns 0 if the endpoint is healthy, recovering, or has no active cooldown.
func (h *HealthTracker) CooldownRemaining() time.Duration {
	if !h.unhealthy.Load() {
		return 0
	}

	rec := h.recoveredAt.Load()
	if rec == 0 {
		return 0
	}

	remaining := time.Until(time.Unix(0, rec))
	if remaining < 0 {
		return 0
	}

	return remaining
}

// Status evaluates and returns the precise current status of the endpoint.
func (h *HealthTracker) Status() HealthStatus {
	unhealthy := h.unhealthy.Load()
	fails := h.failCount.Load()

	if !unhealthy {
		if fails > 0 {
			return StatusDegraded
		}

		return StatusHealthy
	}

	if time.Now().UnixNano() >= h.recoveredAt.Load() {
		return StatusRecovering
	}

	return StatusUnhealthy
}

// Reset clears all failure counters and restores the endpoint to StatusHealthy immediately.
// If the endpoint was previously unhealthy, it triggers the onRecovered callback if configured.
func (h *HealthTracker) Reset() {
	wasUnhealthy := h.unhealthy.Load()
	h.failCount.Store(0)
	h.unhealthy.Store(false)
	h.recoveredAt.Store(0)

	if wasUnhealthy && h.onRecovered != nil {
		h.onRecovered(h.name)
	}
}
