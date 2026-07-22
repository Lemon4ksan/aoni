// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package telemetry_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/telemetry"
)

func TestRTTTracker_CapacityFallback(t *testing.T) {
	t.Parallel()

	t.Run("valid_capacity", func(t *testing.T) {
		t.Parallel()

		tracker := telemetry.NewRTTTracker(10)
		for range 20 {
			tracker.Record(1 * time.Millisecond)
		}

		assert.Equal(t, 10, tracker.Count())
	})

	t.Run("invalid_capacity_fallback", func(t *testing.T) {
		t.Parallel()

		tracker := telemetry.NewRTTTracker(-5)
		for range 200 {
			tracker.Record(1 * time.Millisecond)
		}

		assert.Equal(t, 100, tracker.Count())
	})
}

func TestRTTTracker_Record_And_Averages(t *testing.T) {
	t.Parallel()

	tracker := telemetry.NewRTTTracker(5)

	// 1. Ignoring non-positive RTT values
	tracker.Record(-10 * time.Millisecond)
	tracker.Record(0)
	assert.Equal(t, 0, tracker.Count())

	// 2. Add first valid sample (verifies smoothedRTT initial state)
	tracker.Record(10 * time.Millisecond)
	assert.Equal(t, 1, tracker.Count())
	assert.Equal(t, 10*time.Millisecond, tracker.MinRTT())
	assert.Equal(t, 10*time.Millisecond, tracker.MaxRTT())
	assert.Equal(t, 10*time.Millisecond, tracker.SmoothedRTT())
	assert.Equal(t, 10*time.Millisecond, tracker.AverageRTT())

	// 3. Add second sample
	tracker.Record(20 * time.Millisecond)
	assert.Equal(t, 2, tracker.Count())
	assert.Equal(t, 10*time.Millisecond, tracker.MinRTT())
	assert.Equal(t, 20*time.Millisecond, tracker.MaxRTT())
	// EWMA: 0.9 * 10ms + 0.1 * 20ms = 11ms
	assert.Equal(t, 11*time.Millisecond, tracker.SmoothedRTT())
	assert.Equal(t, 15*time.Millisecond, tracker.AverageRTT())

	// 4. Fill to capacity and verify wrap-around
	tracker.Record(30 * time.Millisecond)
	tracker.Record(40 * time.Millisecond)
	tracker.Record(50 * time.Millisecond)
	assert.Equal(t, 5, tracker.Count())

	// Overflow
	tracker.Record(5 * time.Millisecond) // replaces the first sample (10ms)
	assert.Equal(t, 5, tracker.Count())
	assert.Equal(t, 5*time.Millisecond, tracker.MinRTT())
	assert.Equal(t, 50*time.Millisecond, tracker.MaxRTT())
	assert.Equal(t, 29*time.Millisecond, tracker.AverageRTT()) // (5+20+30+40+50) / 5 = 29
}

func TestRTTTracker_Percentiles_And_Caching(t *testing.T) {
	t.Parallel()

	tracker := telemetry.NewRTTTracker(5)

	// Percentiles, Max, and Average on empty tracker must return 0
	assert.Equal(t, time.Duration(0), tracker.Percentile(95))
	assert.Equal(t, time.Duration(0), tracker.MaxRTT())
	assert.Equal(t, time.Duration(0), tracker.AverageRTT())

	// Fill with distinct values
	tracker.Record(10 * time.Millisecond)
	tracker.Record(20 * time.Millisecond)

	// First calculation: sorts the slice and caches results
	p50First := tracker.Percentile(50)
	assert.Equal(t, 10*time.Millisecond, p50First)

	// Second calculation: returns cached value directly (O(1), zero allocations)
	p50Second := tracker.Percentile(50)
	assert.Equal(t, p50First, p50Second)

	// Add new record: invalidates cache state
	tracker.Record(5 * time.Millisecond)

	// Third calculation: re-sorts and updates cache
	p50New := tracker.Percentile(50)
	assert.Equal(t, 10*time.Millisecond, p50New) // sorted: [5, 10, 20], ceil(0.5 * 3) - 1 = 1 (10ms)

	// Test boundary and overflow inputs
	assert.Equal(t, 5*time.Millisecond, tracker.Percentile(-10))
	assert.Equal(t, 20*time.Millisecond, tracker.Percentile(150))
	assert.Equal(t, 20*time.Millisecond, tracker.P95())
	assert.Equal(t, 20*time.Millisecond, tracker.P99())
}

func TestRTTTracker_Reset(t *testing.T) {
	t.Parallel()

	tracker := telemetry.NewRTTTracker(10)
	tracker.Record(50 * time.Millisecond)
	tracker.Record(100 * time.Millisecond)

	// Trigger calculation to build cache slice
	_ = tracker.Percentile(95)

	tracker.Reset()
	assert.Equal(t, 0, tracker.Count())
	assert.Equal(t, time.Duration(0), tracker.MinRTT())
	assert.Equal(t, time.Duration(0), tracker.MaxRTT())
	assert.Equal(t, time.Duration(0), tracker.AverageRTT())
}

func TestRTTTracker_Concurrency(t *testing.T) {
	t.Parallel()

	tracker := telemetry.NewRTTTracker(50)

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)

		go func(val int) {
			defer wg.Done()

			tracker.Record(time.Duration(val) * time.Millisecond)
			_ = tracker.P95()
			_ = tracker.SmoothedRTT()
			_ = tracker.AverageRTT()
		}(i)
	}

	wg.Wait()
	assert.Equal(t, 50, tracker.Count())
}

func TestDynamicHedgingConfig_ComputeDelay(t *testing.T) {
	t.Parallel()

	t.Run("nil_tracker_or_low_samples", func(t *testing.T) {
		t.Parallel()

		cfgNil := telemetry.DynamicHedgingConfig{Tracker: nil, MinDelay: 30 * time.Millisecond}
		assert.Equal(t, 30*time.Millisecond, cfgNil.ComputeDelay())

		cfgNilFallback := telemetry.DynamicHedgingConfig{Tracker: nil, MinDelay: 0}
		assert.Equal(t, 50*time.Millisecond, cfgNilFallback.ComputeDelay())

		tracker := telemetry.NewRTTTracker(20)
		tracker.Record(100 * time.Millisecond)
		cfgLow := telemetry.DynamicHedgingConfig{Tracker: tracker, MinDelay: 40 * time.Millisecond}
		assert.Equal(t, 40*time.Millisecond, cfgLow.ComputeDelay())
	})

	t.Run("valid_samples_calculates_delay", func(t *testing.T) {
		t.Parallel()

		tracker := telemetry.NewRTTTracker(20)
		for range 10 {
			tracker.Record(100 * time.Millisecond)
		}

		cfg := telemetry.DynamicHedgingConfig{
			Tracker:    tracker,
			Percentile: 95,
			MinDelay:   50 * time.Millisecond,
			MaxDelay:   2 * time.Second,
			Multiplier: 1.5,
		}
		assert.Equal(t, 150*time.Millisecond, cfg.ComputeDelay())
	})

	t.Run("default_fallbacks_and_boundaries", func(t *testing.T) {
		t.Parallel()

		tracker := telemetry.NewRTTTracker(20)
		for range 10 {
			tracker.Record(100 * time.Millisecond)
		}

		cfgZero := telemetry.DynamicHedgingConfig{Tracker: tracker}
		assert.Equal(t, 150*time.Millisecond, cfgZero.ComputeDelay())

		cfgMinCap := telemetry.DynamicHedgingConfig{
			Tracker:    tracker,
			Multiplier: 0.1, // 100ms * 0.1 = 10ms
			MinDelay:   80 * time.Millisecond,
		}
		assert.Equal(t, 80*time.Millisecond, cfgMinCap.ComputeDelay())

		cfgMaxCap := telemetry.DynamicHedgingConfig{
			Tracker:    tracker,
			Multiplier: 10.0, // 100ms * 10 = 1000ms
			MaxDelay:   300 * time.Millisecond,
		}
		assert.Equal(t, 300*time.Millisecond, cfgMaxCap.ComputeDelay())
	})
}

func TestHARGenerator_Record_And_Export(t *testing.T) {
	t.Parallel()

	t.Run("nil_response_exits_safely", func(t *testing.T) {
		t.Parallel()

		gen := telemetry.NewHARGenerator()
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
		require.NoError(t, err)

		gen.Record(req, nil, time.Now(), 100)

		data, err := gen.Export()
		require.NoError(t, err)
		assert.Contains(t, string(data), `"entries": []`)
	})

	t.Run("records_full_request_and_response", func(t *testing.T) {
		t.Parallel()

		gen := telemetry.NewHARGenerator()
		req, err := http.NewRequestWithContext(
			context.Background(),
			http.MethodGet,
			"http://example.com/api?a=1&b=2",
			nil,
		)
		require.NoError(t, err)

		req.Header.Set("User-Agent", "TestAgent")
		req.AddCookie(&http.Cookie{Name: "req-cookie", Value: "c1"})

		resp := &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Proto:         "HTTP/1.1",
			Header:        http.Header{"Content-Type": []string{"application/json"}},
			Body:          io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
			ContentLength: -1,
		}
		resp.Header.Set("Set-Cookie", "resp-cookie=c2; Path=/; Domain=example.com; Max-Age=3600")

		gen.Record(req, resp, time.Now(), 120)

		// Verify body remains readable
		bodyBytes, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, `{"status":"ok"}`, string(bodyBytes))

		data, err := gen.Export()
		require.NoError(t, err)

		harStr := string(data)

		assert.Contains(t, harStr, `"name": "a"`)
		assert.Contains(t, harStr, `"value": "1"`)
		assert.Contains(t, harStr, `"name": "req-cookie"`)
		assert.Contains(t, harStr, `"name": "resp-cookie"`)
		assert.Contains(t, harStr, `"text": "{\"status\":\"ok\"}"`)
	})

	t.Run("large_body_is_truncated_defensively", func(t *testing.T) {
		t.Parallel()

		gen := telemetry.NewHARGenerator()
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
		require.NoError(t, err)

		largePayload := strings.Repeat("A", 150*1024+10)
		resp := &http.Response{
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Type": []string{"text/plain"}},
			Body:          io.NopCloser(strings.NewReader(largePayload)),
			ContentLength: -1,
		}

		gen.Record(req, resp, time.Now(), 100)

		data, err := gen.Export()
		require.NoError(t, err)
		assert.Contains(t, string(data), "Truncated: Response too large for HAR log")
	})

	t.Run("binary_body_is_skipped_defensively", func(t *testing.T) {
		t.Parallel()

		gen := telemetry.NewHARGenerator()
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
		require.NoError(t, err)

		resp := &http.Response{
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Type": []string{"image/png"}},
			Body:          io.NopCloser(strings.NewReader("\x89PNG\r\n\x1a\n")),
			ContentLength: 8,
		}

		gen.Record(req, resp, time.Now(), 100)

		data, err := gen.Export()
		require.NoError(t, err)
		assert.Contains(t, string(data), "Skipped: Binary or large response body")
	})
}
