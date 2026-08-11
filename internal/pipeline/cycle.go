// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"context"
	stdio "io"
	"sync"
	"time"
)

// ExecutionCycle manages the transactional lifecycle state machine of an HTTP request.
type ExecutionCycle struct {
	ctx          context.Context
	cancel       context.CancelFunc
	StartTime    time.Time
	MaxAttempts  int
	AttemptCount int
	RedirectHops int
	MaxRedirects int
	mu           sync.Mutex
	cleanupStack []stdio.Closer
}

// NewExecutionCycle instantiates an [ExecutionCycle] with deadline timeouts and redirect limits.
func NewExecutionCycle(
	parentCtx context.Context,
	timeout time.Duration,
	maxAttempts, maxRedirects int,
) (*ExecutionCycle, context.Context) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	if maxRedirects <= 0 {
		maxRedirects = 10
	}

	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parentCtx, timeout)
	} else {
		ctx, cancel = context.WithCancel(parentCtx)
	}

	cycle := &ExecutionCycle{
		ctx:          ctx,
		cancel:       cancel,
		StartTime:    time.Now(),
		MaxAttempts:  maxAttempts,
		MaxRedirects: maxRedirects,
		cleanupStack: make([]stdio.Closer, 0, 4),
	}

	return cycle, ctx
}

// NextAttempt increments the attempt counter and reports whether retries remain.
func (c *ExecutionCycle) NextAttempt() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.AttemptCount >= c.MaxAttempts {
		return false
	}

	c.AttemptCount++

	return true
}

// RegisterCleanup pushes a resource closer onto the cycle cleanup stack.
func (c *ExecutionCycle) RegisterCleanup(closer stdio.Closer) {
	if closer == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.cleanupStack = append(c.cleanupStack, closer)
}

// Finish releases all registered resources and cancels cycle contexts.
func (c *ExecutionCycle) Finish() {
	if c == nil {
		return
	}

	c.mu.Lock()
	closers := c.cleanupStack
	c.cleanupStack = nil
	c.mu.Unlock()

	for _, cl := range closers {
		if cl != nil {
			_ = cl.Close()
		}
	}

	if c.cancel != nil {
		c.cancel()
	}
}

// Elapsed returns the duration since the cycle started.
func (c *ExecutionCycle) Elapsed() time.Duration {
	return time.Since(c.StartTime)
}
