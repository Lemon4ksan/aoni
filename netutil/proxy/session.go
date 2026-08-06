// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"crypto/tls"
	"sync"

	utls "github.com/refraction-networking/utls"
)

// SessionCache wraps the uTLS [utls.ClientSessionCache] and automatically
// invalidates cached TLS session tickets when the active proxy or source IP changes.
// This prevents server-side tracking of a client across different exit IPs
// via session ticket correlation.
type SessionCache struct {
	mu         sync.RWMutex
	utlsInner  utls.ClientSessionCache
	stdInner   tls.ClientSessionCache
	currentKey string
}

// NewProxyAwareSessionCache creates a new [SessionCache].
func NewProxyAwareSessionCache() *SessionCache {
	return &SessionCache{
		utlsInner: utls.NewLRUClientSessionCache(256),
		stdInner:  tls.NewLRUClientSessionCache(256),
	}
}

// Get retrieves a cached session for the given server name.
// If the session was cached under a different proxy key, it returns nil
// to force a fresh handshake.
func (c *SessionCache) Get(serverName string) (*utls.ClientSessionState, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.utlsInner != nil {
		return c.utlsInner.Get(serverName)
	}

	return nil, false
}

// Put stores a uTLS session ticket.
func (c *SessionCache) Put(serverName string, session *utls.ClientSessionState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.utlsInner != nil {
		c.utlsInner.Put(serverName, session)
	}
}

// StdTLSSessionCache returns an adapter satisfying the standard "crypto/tls".ClientSessionCache interface.
func (c *SessionCache) StdTLSSessionCache() tls.ClientSessionCache {
	return &stdTLSCacheAdapter{cache: c}
}

// SetProxyKey flushes all cached sessions and reinitializes caches when switching proxy endpoints.
func (c *SessionCache) SetProxyKey(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.currentKey == key {
		return
	}

	c.utlsInner = utls.NewLRUClientSessionCache(256)
	c.stdInner = tls.NewLRUClientSessionCache(256)
	c.currentKey = key
}

// CurrentProxyKey returns the active proxy identifier.
func (c *SessionCache) CurrentProxyKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.currentKey
}

// Clear flushes all cached TLS session tickets immediately.
func (c *SessionCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.utlsInner = utls.NewLRUClientSessionCache(256)
	c.stdInner = tls.NewLRUClientSessionCache(256)
}

type stdTLSCacheAdapter struct {
	cache *SessionCache
}

func (a *stdTLSCacheAdapter) Get(serverName string) (*tls.ClientSessionState, bool) {
	a.cache.mu.RLock()
	defer a.cache.mu.RUnlock()

	if a.cache.stdInner != nil {
		return a.cache.stdInner.Get(serverName)
	}

	return nil, false
}

func (a *stdTLSCacheAdapter) Put(serverName string, session *tls.ClientSessionState) {
	a.cache.mu.Lock()
	defer a.cache.mu.Unlock()

	if a.cache.stdInner != nil {
		a.cache.stdInner.Put(serverName, session)
	}
}
