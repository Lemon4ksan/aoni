// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package etag implements RFC 9111 conditional HTTP caching and 304 Not Modified automation.
package etag

import (
	"bytes"
	"io"
	"net/http"
	"sync"

	"github.com/lemon4ksan/foundation/generic"
)

type cachedETagEntry struct {
	etag   string
	status string
	proto  string
	header http.Header
	body   []byte
}

const DefaultMaxEntries = 1024

// Automaton manages ETag recording, If-None-Match header injection, and 304 body reconstruction.
type Automaton struct {
	mu         sync.RWMutex
	maxEntries int
	entries    map[string]cachedETagEntry
}

// NewAutomaton creates a new RFC 9111 [Automaton] instance with default capacity (1024).
func NewAutomaton() *Automaton {
	return NewAutomatonWithCapacity(DefaultMaxEntries)
}

// NewAutomatonWithCapacity creates a new RFC 9111 [Automaton] with the specified capacity limit.
func NewAutomatonWithCapacity(maxEntries int) *Automaton {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}

	return &Automaton{
		maxEntries: maxEntries,
		entries:    make(map[string]cachedETagEntry, maxEntries),
	}
}

// DefaultAutomaton is the package-level shared ETag automaton instance.
var DefaultAutomaton = NewAutomaton()

// Record stores the ETag and response payload bytes for the specified cache key.
func (a *Automaton) Record(key, etagVal string, resp *http.Response, bodyBytes []byte) {
	if etagVal == "" || len(bodyBytes) == 0 {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Bound memory: evict an entry if capacity reached
	if len(a.entries) >= a.maxEntries {
		for k := range a.entries {
			delete(a.entries, k)
			break
		}
	}

	a.entries[key] = cachedETagEntry{
		etag:   etagVal,
		status: resp.Status,
		proto:  resp.Proto,
		header: resp.Header.Clone(),
		body:   bodyBytes,
	}
}

// GetETag returns the stored ETag for key, or empty string if not found.
func (a *Automaton) GetETag(key string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.entries[key].etag
}

// Reconstruct304 returns a 200 OK http.Response populated with the previously cached payload bytes for key.
// Returns nil if key is not found in the automaton cache.
func (a *Automaton) Reconstruct304(key string) *http.Response {
	a.mu.RLock()
	entry, ok := a.entries[key]
	a.mu.RUnlock()

	if !ok {
		return nil
	}

	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Proto:         entry.proto,
		Header:        entry.header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(entry.body)),
		ContentLength: int64(len(entry.body)),
	}
}

// GetETagOptional returns the stored ETag wrapped in generic.Optional.
func (a *Automaton) GetETagOptional(key string) generic.Optional[string] {
	a.mu.RLock()
	defer a.mu.RUnlock()

	entry, ok := a.entries[key]
	if !ok || entry.etag == "" {
		return generic.None[string]()
	}

	return generic.Some(entry.etag)
}

// Reconstruct304Optional returns a reconstructed 200 OK http.Response wrapped in generic.Optional.
//
//nolint:bodyclose // Caller is responsible for closing the reconstructed response body.
func (a *Automaton) Reconstruct304Optional(key string) generic.Optional[*http.Response] {
	resp := a.Reconstruct304(key)
	if resp == nil {
		return generic.None[*http.Response]()
	}

	return generic.Some(resp)
}
