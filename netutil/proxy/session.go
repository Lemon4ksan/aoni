// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"sync"

	utls "github.com/refraction-networking/utls"
)

// SessionCache wraps the uTLS [utls.ClientSessionCache] and automatically
// invalidates cached TLS session tickets when the active proxy or source IP changes.
// This prevents server-side tracking of a client across different exit IPs
// via session ticket correlation.
type SessionCache struct {
	mu         sync.RWMutex
	inner      utls.ClientSessionCache
	currentKey string
}

// NewProxyAwareSessionCache creates a new [SessionCache].
func NewProxyAwareSessionCache() *SessionCache {
	return &SessionCache{
		inner: utls.NewLRUClientSessionCache(256),
	}
}

// Get retrieves a cached session for the given server name.
// If the session was cached under a different proxy key, it returns nil
// to force a fresh handshake.
func (c *SessionCache) Get(serverName string) (*utls.ClientSessionState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.inner != nil {
		return c.inner.Get(serverName)
	}

	return nil, false
}

// Put stores a TLS session ticket.
func (c *SessionCache) Put(serverName string, session *utls.ClientSessionState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.inner != nil {
		c.inner.Put(serverName, session)
	}
}

// SetProxyKey invalidates all cached sessions and starts a fresh session cache
// for the given proxy key (typically the proxy address or source IP).
// This ensures that when the proxy changes, no session tickets from the
// previous proxy are reused, preventing session correlation tracking.
func (c *SessionCache) SetProxyKey(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.currentKey == key {
		return
	}

	// Discard the old cache entirely and start fresh.
	c.inner = utls.NewLRUClientSessionCache(256)
	c.currentKey = key
}

// CurrentProxyKey returns the currently active proxy key.
func (c *SessionCache) CurrentProxyKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentKey
}

// Clear manually flushes all currently cached TLS sessions.
func (c *SessionCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.inner = utls.NewLRUClientSessionCache(256)
}
