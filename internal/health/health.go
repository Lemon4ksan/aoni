// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

// Package health provides internal endpoint health tracking for load balancers and proxy rotators.
package health

import (
	"sync/atomic"
	"time"
)

// Status represents the execution health state of a tracked network endpoint.
type Status int

const (
	// StatusHealthy indicates the endpoint is fully operational with zero consecutive failures.
	StatusHealthy Status = iota
	// StatusDegraded indicates the endpoint experienced failures but remains below the threshold.
	StatusDegraded
	// StatusUnhealthy indicates the endpoint exceeded max failures and is currently in cooldown.
	StatusUnhealthy
	// StatusRecovering indicates the cooldown period elapsed and the endpoint is ready for a trial probe.
	StatusRecovering
)

// String returns a human-readable representation of [Status].
func (s Status) String() string {
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

// Tracker monitors endpoint reliability via consecutive failure thresholds
// and handles automatic state restoration after cooldown delays.
// Safe for concurrent use across multiple goroutines without mutex contention.
type Tracker struct {
	failCount   atomic.Uint32
	unhealthy   atomic.Bool
	recoveredAt atomic.Int64

	maxFails    uint32
	retryAfter  time.Duration
	name        string
	onUnhealthy func(name string, fails uint32, retryAfter time.Duration)
	onRecovered func(name string)
}

// NewTracker creates a [Tracker] configured with failure thresholds and state callbacks.
func NewTracker(
	name string,
	maxFails uint32,
	retryAfter time.Duration,
	onUnhealthy func(string, uint32, time.Duration),
	onRecovered func(string),
) *Tracker {
	return &Tracker{
		name:        name,
		maxFails:    maxFails,
		retryAfter:  retryAfter,
		onUnhealthy: onUnhealthy,
		onRecovered: onRecovered,
	}
}

// MarkFailed increments the consecutive failure counter. If failures reach maxFails,
// the endpoint transitions to unhealthy state and triggers the onUnhealthy callback.
func (h *Tracker) MarkFailed() {
	fails := h.failCount.Add(1)
	if fails >= h.maxFails {
		h.recoveredAt.Store(time.Now().Add(h.retryAfter).UnixNano())

		if h.unhealthy.CompareAndSwap(false, true) && h.onUnhealthy != nil {
			h.onUnhealthy(h.name, fails, h.retryAfter)
		}
	}
}

// MarkSuccess resets the consecutive failure counter and transitions the endpoint back to healthy.
// Triggers onRecovered callback if the endpoint was previously marked unhealthy.
func (h *Tracker) MarkSuccess() {
	h.failCount.Store(0)

	if h.unhealthy.CompareAndSwap(true, false) && h.onRecovered != nil {
		h.onRecovered(h.name)
	}
}

// IsAvailable reports whether the endpoint is eligible to receive network traffic.
// An unhealthy endpoint becomes available again once its cooldown duration elapses.
func (h *Tracker) IsAvailable() bool {
	if !h.unhealthy.Load() {
		return true
	}

	return time.Now().UnixNano() >= h.recoveredAt.Load()
}

// FailCount returns the current count of consecutive recorded failures.
func (h *Tracker) FailCount() uint32 {
	return h.failCount.Load()
}

// CooldownRemaining calculates the duration left before an unhealthy endpoint can attempt recovery.
// Returns 0 if the endpoint is healthy, recovering, or has no active cooldown.
func (h *Tracker) CooldownRemaining() time.Duration {
	if !h.unhealthy.Load() {
		return 0
	}

	rec := h.recoveredAt.Load()
	if rec == 0 {
		return 0
	}

	remaining := time.Until(time.Unix(0, rec))

	return max(remaining, 0)
}

// Status evaluates and returns the current [Status] of the endpoint.
func (h *Tracker) Status() Status {
	if !h.unhealthy.Load() {
		if h.failCount.Load() > 0 {
			return StatusDegraded
		}

		return StatusHealthy
	}

	if time.Now().UnixNano() >= h.recoveredAt.Load() {
		return StatusRecovering
	}

	return StatusUnhealthy
}

// Reset clears failure statistics and forces the endpoint back to StatusHealthy immediately.
func (h *Tracker) Reset() {
	h.failCount.Store(0)
	h.recoveredAt.Store(0)

	if h.unhealthy.CompareAndSwap(true, false) && h.onRecovered != nil {
		h.onRecovered(h.name)
	}
}
