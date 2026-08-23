// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/generic"
)

// LintCacheEntry stores validation state for a single contract file.
type LintCacheEntry struct {
	Hash        string    `json:"hash"`
	IssueCount  int       `json:"issue_count"`
	ValidatedAt time.Time `json:"validated_at"`
}

// LintCache represents the persisted linter cache in .vortex/cache/lint.json.
type LintCache struct {
	mu      sync.RWMutex
	Entries map[string]LintCacheEntry `json:"entries"`
}

// LoadLintCache loads cached validation results from .vortex/cache/lint.json.
func LoadLintCache(rootDir string) (*LintCache, error) {
	cachePath := filepath.Join(rootDir, ".vortex", "cache", "lint.json")

	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &LintCache{Entries: make(map[string]LintCacheEntry)}, nil
		}

		return nil, err
	}

	var lc LintCache
	if err := json.Unmarshal(data, &lc); err != nil {
		return nil, fmt.Errorf("parsing lint cache: %w", err)
	}

	if lc.Entries == nil {
		lc.Entries = make(map[string]LintCacheEntry)
	}

	return &lc, nil
}

// Save writes the linter cache back to disk.
func (lc *LintCache) Save(rootDir string) error {
	dir := filepath.Join(rootDir, ".vortex", "cache")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	var (
		data []byte
		err  error
	)

	generic.WithRLock(&lc.mu, func() {
		data, err = json.MarshalIndent(lc, "", "  ")
	})

	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, "lint.json"), data, 0o600)
}

// IsFresh checks if a file's content hash matches the cached hash with 0 issues.
func (lc *LintCache) IsFresh(relPath string, content []byte) bool {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	entry, exists := lc.Entries[filepath.ToSlash(relPath)]
	if !exists {
		return false
	}

	currentHash := HashBytes(content)
	return entry.Hash == currentHash && entry.IssueCount == 0
}

// Put records a validation result in the cache.
func (lc *LintCache) Put(relPath string, content []byte, issueCount int) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	lc.Entries[filepath.ToSlash(relPath)] = LintCacheEntry{
		Hash:        HashBytes(content),
		IssueCount:  issueCount,
		ValidatedAt: time.Now(),
	}
}

// HashBytes computes the SHA256 hex string of content.
func HashBytes(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}
