// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package engine encapsulates the core execution runtime state, precomputed configurations,
// transaction lifecycle cycles, connection pool janitors, protocol dispatchers, Alt-Svc caches,
// buffer pools, and Referer state automatons for the aoni HTTP client.
package engine

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Engine is the central runtime execution engine orchestrator.
type Engine struct {
	Prepared   PreparedConfig
	Janitor    *Janitor
	Dispatcher *Dispatcher
	AltSvc     *AltSvcCache
	Referer    *RefererAutomaton
	BufferPool *BufferPool
}

// NewEngine constructs a central execution [Engine].
func NewEngine(
	baseURL *url.URL,
	staticHeaders http.Header,
	evictor IdleEvictor,
	evictInterval time.Duration,
	maxInflight int,
) *Engine {
	altSvc := NewAltSvcCache()

	return &Engine{
		Prepared:   NewPreparedConfig(baseURL, staticHeaders),
		Janitor:    NewJanitor(evictor, evictInterval, maxInflight),
		Dispatcher: NewDispatcher(altSvc),
		AltSvc:     altSvc,
		Referer:    NewRefererAutomaton(PolicyStrictOriginWhenCrossOrigin),
		BufferPool: GlobalBufferPool,
	}
}

// NewCycle starts a transaction execution cycle tied to this engine.
func (e *Engine) NewCycle(
	parentCtx context.Context,
	timeout time.Duration,
	maxAttempts, maxRedirects int,
) (*ExecutionCycle, context.Context) {
	return NewExecutionCycle(parentCtx, timeout, maxAttempts, maxRedirects)
}

// Close shuts down background janitors and releases resources.
func (e *Engine) Close() {
	if e == nil {
		return
	}

	if e.Janitor != nil {
		e.Janitor.Stop()
	}
}
