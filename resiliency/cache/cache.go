// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cache provides in-memory HTTP response caching implementations for the aoni pipeline
// conforming to RFC 9111 (HTTP Caching) with automated background TTL eviction.
package cache

import (
	"context"
	"errors"
	"hash/maphash"
	"slices"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/timekit"
)

// ErrCacheMiss is returned when a requested HTTP response is not found in the cache or has expired per RFC 9111 §3.
var ErrCacheMiss = errors.New("aoni/cache: miss")

// Store defines the persistence contract for strongly typed generic caching backends.
type Store[K comparable, V any] interface {
	// Get retrieves cached value by key.
	Get(ctx context.Context, key K) (V, error)
	// Set stores cached value by key with ttl expiration.
	Set(ctx context.Context, key K, val V, ttl time.Duration) error
}

// InMemoryStore provides a thread-safe, in-memory cache backend with background janitor cleanup.
// All methods are safe for concurrent access across multiple goroutines.
type InMemoryStore[K comparable, V any] struct {
	mu     sync.RWMutex
	items  map[K]genericEntry[V]
	cancel context.CancelFunc
}

type genericEntry[V any] struct {
	value     V
	expiresAt time.Time
}

// New creates a new generic [InMemoryStore] with automatic background eviction.
func New[K comparable, V any](cleanupInterval time.Duration) *InMemoryStore[K, V] {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec
	store := &InMemoryStore[K, V]{
		items:  make(map[K]genericEntry[V]),
		cancel: cancel,
	}

	if cleanupInterval > 0 {
		go store.startEvictionLoop(ctx, cleanupInterval)
	}

	return store
}

// NewInMemoryStore creates a new [InMemoryStore] configured for HTTP byte payloads.
func NewInMemoryStore(cleanupInterval time.Duration) *InMemoryStore[any, []byte] {
	return New[any, []byte](cleanupInterval)
}

// Get retrieves a copy of cached item for key. Returns [ErrCacheMiss] if missing or expired.
func (s *InMemoryStore[K, V]) Get(_ context.Context, key K) (V, error) {
	s.mu.RLock()
	entry, ok := s.items[key]
	s.mu.RUnlock()

	if !ok || timekit.CoarseNow().After(entry.expiresAt) {
		var zero V
		return zero, ErrCacheMiss
	}

	if b, isBytes := any(entry.value).([]byte); isBytes {
		return any(slices.Clone(b)).(V), nil
	}

	return entry.value, nil
}

// GetDirect retrieves the cached value directly without cloning byte slices.
func (s *InMemoryStore[K, V]) GetDirect(_ context.Context, key K) (V, error) {
	s.mu.RLock()
	entry, ok := s.items[key]
	s.mu.RUnlock()

	if !ok || timekit.CoarseNow().After(entry.expiresAt) {
		var zero V
		return zero, ErrCacheMiss
	}

	return entry.value, nil
}

// GetOptional retrieves cached payload for key as a [generic.Optional].
func (s *InMemoryStore[K, V]) GetOptional(ctx context.Context, key K) generic.Optional[V] {
	val, err := s.Get(ctx, key)
	if err != nil {
		return generic.None[V]()
	}

	return generic.Some(val)
}

// Set stores value in memory with the specified ttl duration.
func (s *InMemoryStore[K, V]) Set(_ context.Context, key K, val V, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	storedVal := val
	if b, isBytes := any(val).([]byte); isBytes {
		storedVal = any(slices.Clone(b)).(V)
	}

	s.items[key] = genericEntry[V]{
		value:     storedVal,
		expiresAt: timekit.CoarseNow().Add(ttl),
	}

	return nil
}

// startEvictionLoop runs a periodic timer to purge expired entries until context cancellation.
func (s *InMemoryStore[K, V]) startEvictionLoop(ctx context.Context, interval time.Duration) {
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
func (s *InMemoryStore[K, V]) purgeExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, entry := range s.items {
		if now.After(entry.expiresAt) {
			delete(s.items, key)
		}
	}
}

// Close cancels the background janitor loop.
func (s *InMemoryStore[K, V]) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}

const (
	numShards = 32
	shardMask = numShards - 1
)

// ShardedStore provides a partitioned, lock-striped in-memory cache backend
// reducing lock contention across multiple CPU cores by distributing keys across 32 independent shards.
type ShardedStore[K comparable, V any] struct {
	shards [numShards]cacheShard[K, V]
	seed   maphash.Seed
	cancel context.CancelFunc
}

type cacheShard[K comparable, V any] struct {
	mu    sync.RWMutex
	items map[K]genericEntry[V]
	_     [32]byte // Cache-line padding isolating 64-byte L1 cache lines across CPU cores
}

// NewShardedStore creates a partitioned [ShardedStore] with 32 lock-striped shards and automatic background eviction.
func NewShardedStore[K comparable, V any](cleanupInterval time.Duration) *ShardedStore[K, V] {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec
	store := &ShardedStore[K, V]{
		seed:   maphash.MakeSeed(),
		cancel: cancel,
	}

	for i := range numShards {
		store.shards[i].items = make(map[K]genericEntry[V])
	}

	if cleanupInterval > 0 {
		go store.startEvictionLoop(ctx, cleanupInterval)
	}

	return store
}

// Get retrieves a copy of cached item for key. Returns [ErrCacheMiss] if missing or expired.
func (s *ShardedStore[K, V]) Get(_ context.Context, key K) (V, error) {
	shardIdx := int(maphash.Comparable(s.seed, key) & shardMask)
	shard := &s.shards[shardIdx]

	shard.mu.RLock()
	entry, ok := shard.items[key]
	shard.mu.RUnlock()

	if !ok || timekit.CoarseNow().After(entry.expiresAt) {
		var zero V
		return zero, ErrCacheMiss
	}

	if b, isBytes := any(entry.value).([]byte); isBytes {
		return any(slices.Clone(b)).(V), nil
	}

	return entry.value, nil
}

// GetDirect retrieves the cached value directly without cloning byte slices.
func (s *ShardedStore[K, V]) GetDirect(_ context.Context, key K) (V, error) {
	shardIdx := int(maphash.Comparable(s.seed, key) & shardMask)
	shard := &s.shards[shardIdx]

	shard.mu.RLock()
	entry, ok := shard.items[key]
	shard.mu.RUnlock()

	if !ok || timekit.CoarseNow().After(entry.expiresAt) {
		var zero V
		return zero, ErrCacheMiss
	}

	return entry.value, nil
}

// GetOptional retrieves cached payload for key as a [generic.Optional].
func (s *ShardedStore[K, V]) GetOptional(ctx context.Context, key K) generic.Optional[V] {
	val, err := s.Get(ctx, key)
	if err != nil {
		return generic.None[V]()
	}

	return generic.Some(val)
}

// Set stores value in memory with the specified ttl duration.
func (s *ShardedStore[K, V]) Set(_ context.Context, key K, val V, ttl time.Duration) error {
	storedVal := val
	if b, isBytes := any(val).([]byte); isBytes {
		storedVal = any(slices.Clone(b)).(V)
	}

	shardIdx := int(maphash.Comparable(s.seed, key) & shardMask)
	shard := &s.shards[shardIdx]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	shard.items[key] = genericEntry[V]{
		value:     storedVal,
		expiresAt: timekit.CoarseNow().Add(ttl),
	}

	return nil
}

func (s *ShardedStore[K, V]) startEvictionLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			for i := range numShards {
				s.shards[i].purgeExpired(now)
			}
		}
	}
}

func (s *cacheShard[K, V]) purgeExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, entry := range s.items {
		if now.After(entry.expiresAt) {
			delete(s.items, key)
		}
	}
}

// Close cancels the background janitor loop.
func (s *ShardedStore[K, V]) Close() {
	if s.cancel != nil {
		s.cancel()
	}
}

// LRUStore provides a bounded-memory LRU cache backend with O(1) Get and Put.
type LRUStore[K comparable, V any] struct {
	lru *generic.LRU[K, genericEntry[V]]
}

// NewLRUStore creates a new [LRUStore] with a specified capacity limit.
func NewLRUStore[K comparable, V any](capacity int) *LRUStore[K, V] {
	return &LRUStore[K, V]{
		lru: generic.NewLRU[K, genericEntry[V]](capacity),
	}
}

// Get retrieves a copy of cached item for key from the LRU cache.
func (s *LRUStore[K, V]) Get(_ context.Context, key K) (V, error) {
	entry, ok := s.lru.Get(key)
	if !ok || timekit.CoarseNow().After(entry.expiresAt) {
		if ok {
			s.lru.Delete(key)
		}

		var zero V

		return zero, ErrCacheMiss
	}

	if b, isBytes := any(entry.value).([]byte); isBytes {
		return any(slices.Clone(b)).(V), nil
	}

	return entry.value, nil
}

// Set stores a value in the LRU cache with TTL expiration.
func (s *LRUStore[K, V]) Set(_ context.Context, key K, val V, ttl time.Duration) error {
	storedVal := val
	if b, isBytes := any(val).([]byte); isBytes {
		storedVal = any(slices.Clone(b)).(V)
	}

	s.lru.Put(key, genericEntry[V]{
		value:     storedVal,
		expiresAt: timekit.CoarseNow().Add(ttl),
	})

	return nil
}

// Delete evicts a key from the LRU cache.
func (s *LRUStore[K, V]) Delete(_ context.Context, key K) error {
	s.lru.Delete(key)
	return nil
}

// Len returns the number of active entries in the LRU cache.
func (s *LRUStore[K, V]) Len() int {
	return s.lru.Len()
}
