// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package telemetry

import (
	"hash/crc32"
	"sync"
	"time"

	"github.com/lemon4ksan/aoni/foundation/bytesconv"
	"github.com/lemon4ksan/aoni/foundation/clock"
)

// duplicateEntry stores a timestamped CRC64 request signature in the ring buffer.
type duplicateEntry struct {
	timestamp time.Time
	hash      uint64
}

// DuplicateRequestGuard detects request loops and rapid duplicate dispatches
// using a zero-allocation ring buffer and hardware CRC32 hashing.
type DuplicateRequestGuard struct {
	mu          sync.Mutex
	onDuplicate func(method, rawURL string, elapsed time.Duration)
	entries     []duplicateEntry
	window      time.Duration
	capacity    int
	cursor      int
}

// NewDuplicateRequestGuard creates a DuplicateRequestGuard with the specified capacity and time window.
func NewDuplicateRequestGuard(
	capacity int,
	window time.Duration,
	onDuplicate func(method, rawURL string, elapsed time.Duration),
) *DuplicateRequestGuard {
	if capacity <= 0 {
		capacity = 128
	}

	if window <= 0 {
		window = 10 * time.Second
	}

	return &DuplicateRequestGuard{
		entries:     make([]duplicateEntry, capacity),
		capacity:    capacity,
		window:      window,
		onDuplicate: onDuplicate,
	}
}

// CheckAndRecord calculates the request signature and logs a warning if a duplicate occurred within window.
func (g *DuplicateRequestGuard) CheckAndRecord(method, rawURL string) {
	if g == nil || rawURL == "" {
		return
	}

	hash := computeRequestHash(method, rawURL)
	now := clock.CoarseTime()

	g.mu.Lock()
	defer g.mu.Unlock()

	if elapsed, found := g.findDuplicate(hash, now); found {
		if g.onDuplicate != nil {
			g.onDuplicate(method, rawURL, elapsed)
		}
	}

	g.entries[g.cursor] = duplicateEntry{
		hash:      hash,
		timestamp: now,
	}

	g.cursor = (g.cursor + 1) % g.capacity
}

// findDuplicate scans the ring buffer for a matching request hash within the active time window.
func (g *DuplicateRequestGuard) findDuplicate(hash uint64, now time.Time) (time.Duration, bool) {
	for i := 0; i < g.capacity; i++ {
		entry := g.entries[i]
		if entry.hash == hash && !entry.timestamp.IsZero() {
			elapsed := now.Sub(entry.timestamp)
			if elapsed <= g.window {
				return elapsed, true
			}
		}
	}

	return 0, false
}

// computeRequestHash produces a 64-bit combined CRC32 fingerprint from method and rawURL.
//
//go:inline
func computeRequestHash(method, rawURL string) uint64 {
	h1 := uint64(crc32.ChecksumIEEE(bytesconv.S2B(method)))
	h2 := uint64(crc32.ChecksumIEEE(bytesconv.S2B(rawURL)))

	return (h1 << 32) | h2
}
