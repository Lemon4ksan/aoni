// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"sync"

	utls "github.com/refraction-networking/utls"
)

// ProxyAwareSessionCache wraps the uTLS [utls.ClientSessionCache] and automatically
// invalidates cached TLS session tickets when the active proxy or source IP changes.
// This prevents server-side tracking of a client across different exit IPs
// via session ticket correlation.
type ProxyAwareSessionCache struct {
	mu         sync.RWMutex
	inner      utls.ClientSessionCache
	currentKey string
}

// NewProxyAwareSessionCache creates a new [ProxyAwareSessionCache].
func NewProxyAwareSessionCache() *ProxyAwareSessionCache {
	return &ProxyAwareSessionCache{
		inner: utls.NewLRUClientSessionCache(256),
	}
}

// Get retrieves a cached session for the given server name.
// If the session was cached under a different proxy key, it returns nil
// to force a fresh handshake.
func (c *ProxyAwareSessionCache) Get(serverName string) (*utls.ClientSessionState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.inner != nil {
		return c.inner.Get(serverName)
	}

	return nil, false
}

// Put stores a TLS session ticket.
func (c *ProxyAwareSessionCache) Put(serverName string, session *utls.ClientSessionState) {
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
func (c *ProxyAwareSessionCache) SetProxyKey(key string) {
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
func (c *ProxyAwareSessionCache) CurrentProxyKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentKey
}
