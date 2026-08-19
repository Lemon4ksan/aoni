// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"context"
	"net"
	"sync"
)

// Context defines the execution context for an active SSH connection or session,
// exposing connection metadata and thread-safe key-value storage.
type Context interface {
	context.Context

	User() string
	RemoteAddr() net.Addr
	LocalAddr() net.Addr
	SessionID() string
	SetValue(key, val any)
}

type sshContext struct {
	context.Context

	user       string
	remoteAddr net.Addr
	localAddr  net.Addr
	sessionID  string

	values map[any]any
	mu     sync.RWMutex
}

func newContext(parent context.Context, user string, remote, local net.Addr, sessionID string) Context {
	if parent == nil {
		parent = context.Background()
	}

	return &sshContext{
		Context:    parent,
		user:       user,
		remoteAddr: remote,
		localAddr:  local,
		sessionID:  sessionID,
		values:     make(map[any]any),
	}
}

func (c *sshContext) User() string {
	return c.user
}

func (c *sshContext) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *sshContext) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *sshContext) SessionID() string {
	return c.sessionID
}

func (c *sshContext) SetValue(key, val any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.values[key] = val
}

func (c *sshContext) Value(key any) any {
	c.mu.RLock()
	val, ok := c.values[key]
	c.mu.RUnlock()

	if ok {
		return val
	}

	return c.Context.Value(key)
}
