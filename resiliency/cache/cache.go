// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

// Package cache provides in-memory HTTP response caching implementations for the aoni pipeline.
package cache

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrCacheMiss is returned when a requested HTTP response is not found in the cache or has expired.
var ErrCacheMiss = errors.New("aoni cache: miss")

// InMemoryStore provides a thread-safe, in-memory cache backend with background janitor cleanup.
type InMemoryStore struct {
	mu     sync.RWMutex
	items  map[any]inMemoryEntry
	cancel context.CancelFunc
}

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

// Get retrieves cached bytes for key. Returns [ErrCacheMiss] if missing or expired.
func (s *InMemoryStore) Get(_ context.Context, key any) ([]byte, error) {
	s.mu.RLock()
	entry, ok := s.items[key]
	s.mu.RUnlock()

	if !ok || time.Now().After(entry.expiresAt) {
		return nil, ErrCacheMiss
	}

	return entry.value, nil
}

// Set stores value in memory with the specified ttl duration.
func (s *InMemoryStore) Set(_ context.Context, key any, val []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items[key] = inMemoryEntry{
		value:     val,
		expiresAt: time.Now().Add(ttl),
	}

	return nil
}

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

func (s *InMemoryStore) purgeExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, entry := range s.items {
		if now.After(entry.expiresAt) {
			delete(s.items, key)
		}
	}
}
