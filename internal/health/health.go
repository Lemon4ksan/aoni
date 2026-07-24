// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package health provides endpoint health tracking for load balancers and proxy rotators.
package health

import (
	"sync/atomic"
	"time"
)

// Status represents the operational health state of a tracked network endpoint.
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

// Tracker monitors endpoint reliability via failure thresholds and manages cooldown state recovery.
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

// NewTracker creates a thread-safe [Tracker] configured with failure thresholds and state callbacks.
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

// MarkFailed records a failure event. If consecutive failures reach maxFails, transitions to [StatusUnhealthy].
func (h *Tracker) MarkFailed() {
	fails := h.failCount.Add(1)
	if fails >= h.maxFails {
		h.recoveredAt.Store(time.Now().Add(h.retryAfter).UnixNano())

		if h.unhealthy.CompareAndSwap(false, true) && h.onUnhealthy != nil {
			h.onUnhealthy(h.name, fails, h.retryAfter)
		}
	}
}

// MarkSuccess resets consecutive failure counters and restores endpoint health state to [StatusHealthy].
func (h *Tracker) MarkSuccess() {
	h.failCount.Store(0)

	if h.unhealthy.CompareAndSwap(true, false) && h.onRecovered != nil {
		h.onRecovered(h.name)
	}
}

// IsAvailable reports whether the endpoint is eligible to receive network traffic.
func (h *Tracker) IsAvailable() bool {
	if !h.unhealthy.Load() {
		return true
	}

	return time.Now().UnixNano() >= h.recoveredAt.Load()
}

// FailCount returns the current consecutive recorded failure count.
func (h *Tracker) FailCount() uint32 {
	return h.failCount.Load()
}

// CooldownRemaining calculates the duration remaining before an unhealthy endpoint can attempt trial recovery.
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

// Reset clears failure counters and restores the endpoint to [StatusHealthy] immediately.
func (h *Tracker) Reset() {
	h.failCount.Store(0)
	h.recoveredAt.Store(0)

	if h.unhealthy.CompareAndSwap(true, false) && h.onRecovered != nil {
		h.onRecovered(h.name)
	}
}
