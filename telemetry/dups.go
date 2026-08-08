// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package telemetry

import (
	"sync"
	"time"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

type duplicateEntry struct {
	timestamp time.Time
	hash      uint64
}

// DuplicateRequestGuard detects request loops and rapid duplicate dispatches
// using a zero-allocation ring buffer and FNV-1a 64-bit hashing.
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
	now := time.Now()

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

func (g *DuplicateRequestGuard) findDuplicate(hash uint64, now time.Time) (time.Duration, bool) {
	for i := range g.entries {
		entry := g.entries[i]
		if entry.hash == hash && !entry.timestamp.IsZero() {
			elapsed := now.Sub(entry.timestamp)
			if elapsed < g.window {
				return elapsed, true
			}
		}
	}

	return 0, false
}

func computeRequestHash(method, rawURL string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)

	var h uint64 = offset64

	methodBytes := bytesconv.S2B(method)
	for i := range methodBytes {
		h ^= uint64(methodBytes[i])
		h *= prime64
	}

	h ^= uint64(':')
	h *= prime64

	urlBytes := bytesconv.S2B(rawURL)
	for i := range urlBytes {
		h ^= uint64(urlBytes[i])
		h *= prime64
	}

	return h
}
