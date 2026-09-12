// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"sync"

	"crypto/tls"
)

// SessionCache wraps the uTLS [tls.ClientSessionCache] and automatically
// invalidates cached TLS session tickets when the active proxy or source IP changes.
// This prevents server-side tracking of a client across different exit IPs
// via session ticket correlation.
type SessionCache struct {
	mu         sync.RWMutex
	utlsCaches map[string]tls.ClientSessionCache
	stdCaches  map[string]tls.ClientSessionCache
	currentKey string
}

// NewProxyAwareSessionCache creates a new [SessionCache].
func NewProxyAwareSessionCache() *SessionCache {
	return &SessionCache{
		utlsCaches: make(map[string]tls.ClientSessionCache),
		stdCaches:  make(map[string]tls.ClientSessionCache),
	}
}

func (c *SessionCache) getUtlsCache(key string) tls.ClientSessionCache {
	c.mu.RLock()
	cache := c.utlsCaches[key]
	c.mu.RUnlock()

	if cache != nil {
		return cache
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.utlsCaches[key] == nil {
		c.utlsCaches[key] = tls.NewLRUClientSessionCache(256)
		c.stdCaches[key] = tls.NewLRUClientSessionCache(256)
	}

	return c.utlsCaches[key]
}

func (c *SessionCache) getStdCache(key string) tls.ClientSessionCache {
	c.mu.RLock()
	cache := c.stdCaches[key]
	c.mu.RUnlock()

	if cache != nil {
		return cache
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stdCaches[key] == nil {
		c.utlsCaches[key] = tls.NewLRUClientSessionCache(256)
		c.stdCaches[key] = tls.NewLRUClientSessionCache(256)
	}

	return c.stdCaches[key]
}

// Get retrieves a cached session for the given server name.
func (c *SessionCache) Get(serverName string) (*tls.ClientSessionState, bool) {
	c.mu.RLock()
	key := c.currentKey
	c.mu.RUnlock()

	return c.getUtlsCache(key).Get(serverName)
}

// Put stores a uTLS session ticket.
func (c *SessionCache) Put(serverName string, session *tls.ClientSessionState) {
	c.mu.RLock()
	key := c.currentKey
	c.mu.RUnlock()

	c.getUtlsCache(key).Put(serverName, session)
}

// StdTLSSessionCache returns an adapter satisfying the standard "crypto/tls".ClientSessionCache interface.
func (c *SessionCache) StdTLSSessionCache() tls.ClientSessionCache {
	return &stdTLSCacheAdapter{cache: c}
}

// SetProxyKey sets the default proxy key (used if not cloned).
func (c *SessionCache) SetProxyKey(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

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

	c.utlsCaches = make(map[string]tls.ClientSessionCache)
	c.stdCaches = make(map[string]tls.ClientSessionCache)
}

// CloneWithProxy returns a request-scoped wrapper bound to a specific proxy key,
// avoiding data races and cross-persona leakage on the global cache.
func (c *SessionCache) CloneWithProxy(key string) *SessionCacheWrapper {
	return &SessionCacheWrapper{parent: c, proxyKey: key}
}

type SessionCacheWrapper struct {
	parent   *SessionCache
	proxyKey string
}

func (w *SessionCacheWrapper) Get(serverName string) (*tls.ClientSessionState, bool) {
	return w.parent.getUtlsCache(w.proxyKey).Get(serverName)
}

func (w *SessionCacheWrapper) Put(serverName string, session *tls.ClientSessionState) {
	w.parent.getUtlsCache(w.proxyKey).Put(serverName, session)
}

func (w *SessionCacheWrapper) SetProxyKey(key string) {
	w.proxyKey = key
}

func (w *SessionCacheWrapper) CurrentProxyKey() string {
	return w.proxyKey
}

func (w *SessionCacheWrapper) Clear() {
	w.parent.Clear()
}

func (w *SessionCacheWrapper) StdTLSSessionCache() tls.ClientSessionCache {
	return &stdTLSCacheWrapperAdapter{wrapper: w}
}

type stdTLSCacheAdapter struct {
	cache *SessionCache
}

func (a *stdTLSCacheAdapter) Get(serverName string) (*tls.ClientSessionState, bool) {
	a.cache.mu.RLock()
	key := a.cache.currentKey
	a.cache.mu.RUnlock()

	return a.cache.getStdCache(key).Get(serverName)
}

func (a *stdTLSCacheAdapter) Put(serverName string, session *tls.ClientSessionState) {
	a.cache.mu.RLock()
	key := a.cache.currentKey
	a.cache.mu.RUnlock()
	a.cache.getStdCache(key).Put(serverName, session)
}

type stdTLSCacheWrapperAdapter struct {
	wrapper *SessionCacheWrapper
}

func (a *stdTLSCacheWrapperAdapter) Get(serverName string) (*tls.ClientSessionState, bool) {
	return a.wrapper.parent.getStdCache(a.wrapper.proxyKey).Get(serverName)
}

func (a *stdTLSCacheWrapperAdapter) Put(serverName string, session *tls.ClientSessionState) {
	a.wrapper.parent.getStdCache(a.wrapper.proxyKey).Put(serverName, session)
}
