// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package telemetry_test

import (
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/telemetry"
)

func BenchmarkRTTTracker_Record(b *testing.B) {
	tracker := telemetry.NewRTTTracker(100)
	sample := 15 * time.Millisecond

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		tracker.Record(sample)
	}
}

func BenchmarkRTTTracker_Percentile(b *testing.B) {
	tracker := telemetry.NewRTTTracker(100)
	for i := 1; i <= 100; i++ {
		tracker.Record(time.Duration(i) * time.Millisecond)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = tracker.Percentile(95)
	}
}

func BenchmarkRTTTracker_P95(b *testing.B) {
	tracker := telemetry.NewRTTTracker(100)
	for i := 1; i <= 100; i++ {
		tracker.Record(time.Duration(i) * time.Millisecond)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = tracker.P95()
	}
}

func BenchmarkRTTTracker_AverageRTT(b *testing.B) {
	tracker := telemetry.NewRTTTracker(100)
	for i := 1; i <= 100; i++ {
		tracker.Record(time.Duration(i) * time.Millisecond)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_ = tracker.AverageRTT()
	}
}

func BenchmarkRTTTracker_RecordAndPercentile(b *testing.B) {
	tracker := telemetry.NewRTTTracker(100)
	for i := 1; i <= 100; i++ {
		tracker.Record(time.Duration(i) * time.Millisecond)
	}

	sample := 42 * time.Millisecond

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		tracker.Record(sample)
		_ = tracker.P95()
	}
}
