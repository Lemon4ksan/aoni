// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package session maintains thread-safe atomic identity and credential state for socket sessions.
package session

import (
	"sync/atomic"
)

// State holds a lock-free snapshot of arbitrary session metadata.
type State[T any] struct {
	ptr atomic.Pointer[T]
}

// NewState initializes a new State container.
func NewState[T any](initial *T) *State[T] {
	s := &State[T]{}
	if initial != nil {
		s.ptr.Store(initial)
	}

	return s
}

// Load returns the current snapshot pointer or nil.
func (s *State[T]) Load() *T {
	return s.ptr.Load()
}

// Store sets a new snapshot pointer.
func (s *State[T]) Store(val *T) {
	s.ptr.Store(val)
}

// Swap sets a new snapshot pointer and returns the old one.
func (s *State[T]) Swap(val *T) *T {
	return s.ptr.Swap(val)
}

// BasicSession provides a thread-safe primitive store for session ID and auth token.
type BasicSession struct {
	sessionID atomic.Uint64
	token     atomic.Value
}

// SessionID returns the current numeric session identifier.
func (b *BasicSession) SessionID() uint64 {
	return b.sessionID.Load()
}

// SetSessionID sets the numeric session identifier.
func (b *BasicSession) SetSessionID(id uint64) {
	b.sessionID.Store(id)
}

// Token returns the active authentication token string.
func (b *BasicSession) Token() string {
	val, _ := b.token.Load().(string)
	return val
}

// SetToken sets the active authentication token string.
func (b *BasicSession) SetToken(token string) {
	b.token.Store(token)
}
