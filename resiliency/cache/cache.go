// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cache provides in-memory HTTP response caching implementations for the aoni pipeline
// conforming to RFC 9111 (HTTP Caching) with automated background TTL eviction.
package cache

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/silicon/clock"
)

// ErrCacheMiss is returned when a requested HTTP response is not found in the cache or has expired per RFC 9111 §3.
var ErrCacheMiss = errors.New("aoni/cache: miss")

// InMemoryStore provides a thread-safe, in-memory cache backend with background janitor cleanup conforming to RFC 9111.
// All methods are safe for concurrent access across multiple goroutines.
type InMemoryStore struct {
	mu     sync.RWMutex
	items  map[any]inMemoryEntry
	cancel context.CancelFunc
}

// inMemoryEntry stores cached payload bytes alongside its expiration timestamp.
type inMemoryEntry struct {
	value     []byte
	expiresAt time.Time
}

// NewInMemoryStore creates a new [InMemoryStore] with automatic background eviction.
func NewInMemoryStore(cleanupInterval time.Duration) *InMemoryStore {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec
	store := &InMemoryStore{
		items:  make(map[any]inMemoryEntry),
		cancel: cancel,
	}

	if cleanupInterval > 0 {
		go store.startEvictionLoop(ctx, cleanupInterval)
	}

	return store
}

// Get retrieves a copy of cached bytes for key. Returns [ErrCacheMiss] if missing or expired.
func (s *InMemoryStore) Get(_ context.Context, key any) ([]byte, error) {
	s.mu.RLock()
	entry, ok := s.items[key]
	s.mu.RUnlock()

	if !ok || clock.CoarseTime().After(entry.expiresAt) {
		return nil, ErrCacheMiss
	}

	return slices.Clone(entry.value), nil
}

// Set stores value in memory with the specified ttl duration.
func (s *InMemoryStore) Set(_ context.Context, key any, val []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items[key] = inMemoryEntry{
		value:     slices.Clone(val),
		expiresAt: clock.CoarseTime().Add(ttl),
	}

	return nil
}

// startEvictionLoop runs a periodic timer to purge expired entries until context cancellation.
func (s *InMemoryStore) startEvictionLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.purgeExpired(now)
		}
	}
}

// purgeExpired scans and removes entries whose expiration timestamp is in the past.
func (s *InMemoryStore) purgeExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, entry := range s.items {
		if now.After(entry.expiresAt) {
			delete(s.items, key)
		}
	}
}

// Close cancels the background janitor loop.
func (s *InMemoryStore) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}
