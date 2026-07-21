// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"errors"
	"sync"
	"time"
)

// CacheStore defines an interface for response caching.
type CacheStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
}

// CacheConfig configures response caching store and TTL.
type CacheConfig struct {
	Store      CacheStore
	DefaultTTL time.Duration
}

// InMemoryCacheStore implements CacheStore in memory.
type InMemoryCacheStore struct {
	mu    sync.RWMutex
	cache map[string]inMemoryCacheEntry
}

// NewInMemoryCacheStore creates a thread-safe in-memory CacheStore.
func NewInMemoryCacheStore() *InMemoryCacheStore {
	return &InMemoryCacheStore{
		cache: make(map[string]inMemoryCacheEntry),
	}
}

// Get retrieves cached response bytes from memory.
func (s *InMemoryCacheStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.cache[key]
	if !ok || time.Now().After(entry.ExpiresAt) {
		return nil, errors.New("aoni cache: miss")
	}

	return entry.Value, nil
}

// Set stores response bytes in memory with TTL.
func (s *InMemoryCacheStore) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cache[key] = inMemoryCacheEntry{
		Value:     val,
		ExpiresAt: time.Now().Add(ttl),
	}

	return nil
}

type inMemoryCacheEntry struct {
	Value     []byte
	ExpiresAt time.Time
}

// CachedResponse holds a cached HTTP response.
type CachedResponse struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	BodyBase64 string              `json:"body_base64"`
}
