// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package clock_test

import (
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/foundation/clock"
)

func TestCoarseClock(t *testing.T) {
	nano := clock.CoarseNowNano()
	if nano <= 0 {
		t.Fatalf("expected non-zero coarse nano, got %d", nano)
	}

	ct := clock.CoarseTime()
	if ct.IsZero() {
		t.Fatal("expected non-zero coarse time")
	}

	time.Sleep(5 * time.Millisecond)

	newNano := clock.CoarseNowNano()
	if newNano <= nano {
		t.Fatalf("expected coarse nano to advance, before=%d after=%d", nano, newNano)
	}
}

func BenchmarkCoarseClock(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = clock.CoarseNowNano()
	}
}

func BenchmarkStandardTimeNow(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = time.Now()
	}
}
