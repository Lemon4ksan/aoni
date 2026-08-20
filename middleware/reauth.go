// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package middleware

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/lemon4ksan/aoni"
)

// ErrReAuthFailed is returned when the automated re-authentication callback fails.
var ErrReAuthFailed = errors.New("aoni/middleware: automated re-authentication failed")

// ReAuthConfig specifies triggers and refresh operations for session re-authentication.
type ReAuthConfig struct {
	// Trigger evaluates whether the HTTP response or execution error warrants a re-authentication attempt.
	Trigger func(resp aoni.Response, err error) bool

	// Refresh executes the token, cookie, or session refresh logic.
	// Executed within a singleflight barrier to prevent concurrent thundering herd.
	Refresh func(ctx context.Context) error

	// MaxRetries defines the maximum number of automated re-auth retry attempts (default: 1).
	MaxRetries int
}

type reauthBarrier struct {
	mu       sync.Mutex
	inFlight bool
	waitChan chan struct{}
	lastErr  error
}

func (b *reauthBarrier) execute(ctx context.Context, refreshFn func(context.Context) error) error {
	b.mu.Lock()
	if b.inFlight {
		waitChan := b.waitChan
		b.mu.Unlock()

		select {
		case <-waitChan:
			b.mu.Lock()
			err := b.lastErr
			b.mu.Unlock()

			return err

		case <-ctx.Done():
			return ctx.Err()
		}
	}

	b.inFlight = true
	b.waitChan = make(chan struct{})
	b.mu.Unlock()

	err := refreshFn(ctx)

	b.mu.Lock()
	b.lastErr = err
	b.inFlight = false
	close(b.waitChan)
	b.mu.Unlock()

	return err
}

// ReAuth constructs a thread-safe singleflight re-authentication middleware.
//
// When concurrent in-flight requests encounter a session expiration (defined by Trigger),
// the middleware halts concurrent traffic, runs Refresh exactly once, and replays all
// blocked requests automatically once the refresh succeeds.
func ReAuth(cfg ReAuthConfig) aoni.Middleware {
	if cfg.Trigger == nil {
		cfg.Trigger = func(resp aoni.Response, _ error) bool {
			return resp != nil && resp.StatusCode() == 401
		}
	}

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 1
	}

	barrier := &reauthBarrier{}

	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			resp, err := next.Do(req)

			for attempt := 0; attempt < maxRetries; attempt++ {
				if !cfg.Trigger(resp, err) {
					return resp, err
				}

				if cfg.Refresh == nil {
					return resp, err
				}

				if resp != nil {
					_ = resp.Close()
				}

				refreshErr := barrier.execute(req.Context(), cfg.Refresh)
				if refreshErr != nil {
					return nil, fmt.Errorf("%w: %w", ErrReAuthFailed, refreshErr)
				}

				resp, err = next.Do(req)
			}

			return resp, err
		})
	}
}
