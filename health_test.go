// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthTracker_Transitions(t *testing.T) {
	t.Parallel()

	var (
		unhealthyCount int32
		recoveredCount int32
	)

	onUnhealthy := func(name string, fails uint32, retryAfter time.Duration) {
		atomic.AddInt32(&unhealthyCount, 1)
	}
	onRecovered := func(name string) {
		atomic.AddInt32(&recoveredCount, 1)
	}

	tracker := NewHealthTracker("test-endpoint", 3, 50*time.Millisecond, onUnhealthy, onRecovered)
	require.NotNil(t, tracker)

	// 1. Initial State: Healthy
	assert.Equal(t, StatusHealthy, tracker.Status())
	assert.Equal(t, "Healthy", tracker.Status().String())
	assert.True(t, tracker.IsAvailable())
	assert.Equal(t, uint32(0), tracker.FailCount())
	assert.Equal(t, time.Duration(0), tracker.CooldownRemaining())

	// 2. First failure: Degraded
	tracker.MarkFailed()
	assert.Equal(t, StatusDegraded, tracker.Status())
	assert.Equal(t, "Degraded", tracker.Status().String())
	assert.True(t, tracker.IsAvailable())
	assert.Equal(t, uint32(1), tracker.FailCount())

	// 3. Second failure: Still Degraded
	tracker.MarkFailed()
	assert.Equal(t, StatusDegraded, tracker.Status())
	assert.True(t, tracker.IsAvailable())
	assert.Equal(t, uint32(2), tracker.FailCount())

	// 4. Third failure: Unhealthy (threshold reached)
	tracker.MarkFailed()
	assert.Equal(t, StatusUnhealthy, tracker.Status())
	assert.Equal(t, "Unhealthy", tracker.Status().String())
	assert.False(t, tracker.IsAvailable())
	assert.Equal(t, uint32(3), tracker.FailCount())
	assert.Greater(t, tracker.CooldownRemaining(), time.Duration(0))
	assert.Equal(t, int32(1), atomic.LoadInt32(&unhealthyCount))

	// 5. Cooldown elapses: Recovering
	time.Sleep(70 * time.Millisecond)
	assert.Equal(t, StatusRecovering, tracker.Status())
	assert.Equal(t, "Recovering", tracker.Status().String())
	assert.True(t, tracker.IsAvailable()) // Available for a trial probe
	assert.Equal(t, time.Duration(0), tracker.CooldownRemaining())

	// 6. Successful probe: Healthy again
	tracker.MarkSuccess()
	assert.Equal(t, StatusHealthy, tracker.Status())
	assert.True(t, tracker.IsAvailable())
	assert.Equal(t, uint32(0), tracker.FailCount())
	assert.Equal(t, int32(1), atomic.LoadInt32(&recoveredCount))
}

func TestHealthTracker_Reset(t *testing.T) {
	t.Parallel()

	var recoveredCount int32

	onRecovered := func(name string) {
		atomic.AddInt32(&recoveredCount, 1)
	}

	tracker := NewHealthTracker("test-endpoint", 2, time.Hour, nil, onRecovered)

	// Go to Degraded
	tracker.MarkFailed()
	assert.Equal(t, StatusDegraded, tracker.Status())

	// Reset from Degraded
	tracker.Reset()
	assert.Equal(t, StatusHealthy, tracker.Status())
	assert.Equal(t, int32(0), atomic.LoadInt32(&recoveredCount)) // No onRecovered callback if it wasn't unhealthy

	// Go to Unhealthy
	tracker.MarkFailed()
	tracker.MarkFailed()
	assert.Equal(t, StatusUnhealthy, tracker.Status())

	// Reset from Unhealthy
	tracker.Reset()
	assert.Equal(t, StatusHealthy, tracker.Status())
	assert.Equal(t, int32(1), atomic.LoadInt32(&recoveredCount)) // Triggers onRecovered
}

func TestHealthStatus_StringUnknown(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Unknown", HealthStatus(999).String())
}
