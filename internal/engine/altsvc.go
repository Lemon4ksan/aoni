// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package engine

import (
	"strings"
	"sync"
	"time"
)

type altSvcEntry struct {
	target    string
	expiresAt time.Time
}

// AltSvcCache maintains a thread-safe record of HTTP Alt-Svc protocol advertisements and QUIC/H3 cooldowns.
type AltSvcCache struct {
	mu        sync.RWMutex
	entries   map[string]altSvcEntry
	cooldowns map[string]time.Time
}

// NewAltSvcCache constructs an empty [AltSvcCache].
func NewAltSvcCache() *AltSvcCache {
	return &AltSvcCache{
		entries:   make(map[string]altSvcEntry),
		cooldowns: make(map[string]time.Time),
	}
}

// ParseAndStore parses an Alt-Svc header (e.g. `h3=":443"; ma=86400`) and updates host capability records.
func (c *AltSvcCache) ParseAndStore(host, headerVal string) {
	if c == nil || host == "" || headerVal == "" {
		return
	}

	if !strings.Contains(headerVal, "h3") {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[host] = altSvcEntry{
		target:    "h3",
		expiresAt: time.Now().Add(24 * time.Hour),
	}
}

// HasH3Support reports whether a host has a valid active Alt-Svc advertisement and is not in cooldown.
func (c *AltSvcCache) HasH3Support(host string) bool {
	if c == nil || host == "" {
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if cd, exists := c.cooldowns[host]; exists && time.Now().Before(cd) {
		return false
	}

	entry, exists := c.entries[host]

	return exists && time.Now().Before(entry.expiresAt)
}

// RecordH3Failure marks a host in QUIC/H3 cooldown after connection drops, falling back to H2/H1.
func (c *AltSvcCache) RecordH3Failure(host string, cooldownDuration time.Duration) {
	if c == nil || host == "" {
		return
	}

	if cooldownDuration <= 0 {
		cooldownDuration = 5 * time.Minute
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.cooldowns[host] = time.Now().Add(cooldownDuration)
}
