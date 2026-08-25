// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dict

import (
	"container/list"
	"net/url"
	"sync"
	"time"
)

// StoreOption configures a dictionary storage cache.
type StoreOption func(*Store)

// WithMaxBytes sets the maximum cumulative memory capacity in bytes for cached dictionaries.
func WithMaxBytes(maxBytes int64) StoreOption {
	return func(s *Store) {
		if maxBytes > 0 {
			s.maxBytes = maxBytes
		}
	}
}

// WithMaxMemoryBytes is an alias for [WithMaxBytes].
func WithMaxMemoryBytes(maxBytes int64) StoreOption {
	return WithMaxBytes(maxBytes)
}

// WithMaxDictionarySize sets the maximum size in bytes permitted for a single dictionary.
func WithMaxDictionarySize(maxSize int64) StoreOption {
	return func(s *Store) {
		if maxSize > 0 {
			s.maxDictSize = maxSize
		}
	}
}

// WithDefaultTTL sets the fallback freshness lifetime for dictionaries without an explicit TTL.
func WithDefaultTTL(ttl time.Duration) StoreOption {
	return func(s *Store) {
		if ttl > 0 {
			s.defaultTTL = ttl
		}
	}
}

// Store is a thread-safe, memory-bounded, LRU-evicting compression dictionary cache conforming to RFC 9842.
type Store struct {
	mu          sync.RWMutex
	maxBytes    int64
	maxDictSize int64
	currentSize int64
	defaultTTL  time.Duration

	byHash  map[[32]byte]*list.Element
	byID    map[string]*Dictionary
	lruList *list.List
}

// NewStore creates a new RFC 9842 dictionary cache with the supplied options.
func NewStore(opts ...StoreOption) *Store {
	s := &Store{
		maxBytes:    DefaultMaxStoreBytes,
		maxDictSize: DefaultMaxDictionarySize,
		defaultTTL:  30 * 24 * time.Hour,
		byHash:      make(map[[32]byte]*list.Element),
		byID:        make(map[string]*Dictionary),
		lruList:     list.New(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Store parses the Use-As-Dictionary response header, computes SHA-256 hash, and saves the dictionary.
func (s *Store) Store(respURL *url.URL, header string, body []byte, ttlOverride ...time.Duration) (*Dictionary, error) {
	if s == nil || respURL == nil || len(body) == 0 {
		return nil, ErrInvalidUseAsDictionary
	}

	if int64(len(body)) > s.maxDictSize {
		return nil, ErrDictionaryExpired
	}

	meta, err := ParseUseAsDictionary(header, respURL)
	if err != nil {
		return nil, err
	}

	ttl := meta.TTL
	if ttl <= 0 && len(ttlOverride) > 0 && ttlOverride[0] > 0 {
		ttl = ttlOverride[0]
	}

	if ttl <= 0 {
		ttl = s.defaultTTL
	}

	now := time.Now()
	dict := &Dictionary{
		Hash:      ComputeSHA256(body),
		ID:        meta.ID,
		BaseURL:   respURL,
		Match:     meta.Match,
		MatchDest: meta.MatchDest,
		Type:      meta.Type,
		TTL:       ttl,
		FetchedAt: now,
		ExpiresAt: now.Add(ttl),
		Data:      append([]byte(nil), body...),
	}

	s.Set(dict)

	return dict, nil
}

// Set stores or updates a dictionary in the cache, evicting least-recently used items if capacity is exceeded.
func (s *Store) Set(d *Dictionary) {
	if s == nil || d == nil || len(d.Data) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// If already present by hash, remove old memory accounting
	if elem, exists := s.byHash[d.Hash]; exists {
		old := elem.Value.(*Dictionary)
		s.currentSize -= int64(len(old.Data))
		s.lruList.Remove(elem)

		if old.ID != "" {
			delete(s.byID, old.ID)
		}
	}

	// Enforce memory limits via LRU eviction
	dictSize := int64(len(d.Data))
	for s.currentSize+dictSize > s.maxBytes && s.lruList.Len() > 0 {
		oldest := s.lruList.Back()
		if oldest == nil {
			break
		}

		oldDict := oldest.Value.(*Dictionary)
		s.currentSize -= int64(len(oldDict.Data))
		delete(s.byHash, oldDict.Hash)

		if oldDict.ID != "" {
			delete(s.byID, oldDict.ID)
		}

		s.lruList.Remove(oldest)
	}

	elem := s.lruList.PushFront(d)
	s.byHash[d.Hash] = elem
	s.currentSize += dictSize

	if d.ID != "" {
		s.byID[d.ID] = d
	}
}

// Match finds the best available compression dictionary for the target URL and destination according
// to the strict precedence rules defined in RFC 9842 §2.2.3.
func (s *Store) Match(targetURL *url.URL, dest string) (*Dictionary, bool) {
	if s == nil || targetURL == nil {
		return nil, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	var (
		bestDict     *Dictionary
		bestElem     *list.Element
		bestHasDest  bool
		bestMatchLen int
	)

	for elem := s.lruList.Front(); elem != nil; elem = elem.Next() {
		d := elem.Value.(*Dictionary)

		// Check freshness (RFC 9842 §2.2.1)
		if !d.IsFresh(now) {
			continue
		}

		// Check matching (RFC 9842 §2.2.2)
		if !d.Matches(targetURL, dest) {
			continue
		}

		hasDest := len(d.MatchDest) > 0 && dest != ""
		matchLen := len(d.Match)

		if bestDict == nil {
			bestDict = d
			bestElem = elem
			bestHasDest = hasDest
			bestMatchLen = matchLen

			continue
		}

		// RFC 9842 §2.2.3 Precedence Rules:
		// 1. Specified and matched match-dest takes precedence over no destination.
		if hasDest && !bestHasDest {
			bestDict = d
			bestElem = elem
			bestHasDest = hasDest
			bestMatchLen = matchLen

			continue
		} else if !hasDest && bestHasDest {
			continue
		}

		// 2. Longest match pattern takes precedence.
		if matchLen > bestMatchLen {
			bestDict = d
			bestElem = elem
			bestHasDest = hasDest
			bestMatchLen = matchLen

			continue
		} else if matchLen < bestMatchLen {
			continue
		}

		// 3. Most recently fetched takes precedence.
		if d.FetchedAt.After(bestDict.FetchedAt) {
			bestDict = d
			bestElem = elem
			bestHasDest = hasDest
			bestMatchLen = matchLen
		}
	}

	if bestDict != nil && bestElem != nil {
		s.lruList.MoveToFront(bestElem)
		return bestDict, true
	}

	return nil, false
}

// GetByHash retrieves a dictionary by its SHA-256 hash digest (RFC 9842 §2.2).
func (s *Store) GetByHash(hash [32]byte) (*Dictionary, bool) {
	if s == nil {
		return nil, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	elem, exists := s.byHash[hash]
	if !exists {
		return nil, false
	}

	d := elem.Value.(*Dictionary)
	if !d.IsFresh(time.Now()) {
		return nil, false
	}

	return d, true
}

// GetByID retrieves a dictionary by its server identifier (RFC 9842 §2.1.3 & §2.3).
func (s *Store) GetByID(id string) (*Dictionary, bool) {
	if s == nil || id == "" {
		return nil, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	d, exists := s.byID[id]
	if !exists {
		return nil, false
	}

	if !d.IsFresh(time.Now()) {
		return nil, false
	}

	return d, true
}

// Delete removes a dictionary by its SHA-256 hash digest.
func (s *Store) Delete(hash [32]byte) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if elem, exists := s.byHash[hash]; exists {
		d := elem.Value.(*Dictionary)
		s.currentSize -= int64(len(d.Data))
		delete(s.byHash, hash)

		if d.ID != "" {
			delete(s.byID, d.ID)
		}

		s.lruList.Remove(elem)
	}
}

// Clear purges all stored dictionaries from memory (RFC 9842 §10).
func (s *Store) Clear() {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	clear(s.byHash)
	clear(s.byID)
	s.lruList.Init()
	s.currentSize = 0
}

// Size returns the total count of dictionaries currently stored in the cache.
func (s *Store) Size() int {
	if s == nil {
		return 0
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.byHash)
}

// Bytes returns the total memory consumed by raw dictionary byte arrays.
func (s *Store) Bytes() int64 {
	if s == nil {
		return 0
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.currentSize
}
