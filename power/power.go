// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package power provides utilities for monitoring system power state and detecting clock jumps.
package power

import (
	"context"
	"slices"
	"time"

	"github.com/lemon4ksan/foundation/generic"
)

// Watcher detects OS sleep and resume events using clock-jump analysis,
// purging stale zombie sockets and connection pools across system power transitions.
type Watcher struct {
	onSuspend     generic.Safe[[]func()]
	onResume      generic.Safe[[]func()]
	cancel        context.CancelFunc
	jumpThreshold time.Duration
}

// NewWatcher constructs a PowerWatcher monitoring system sleep transitions.
func NewWatcher(jumpThreshold time.Duration) *Watcher {
	if jumpThreshold <= 0 {
		jumpThreshold = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	w := &Watcher{
		jumpThreshold: jumpThreshold,
		cancel:        cancel,
	}

	go w.monitorClockJumps(ctx)

	return w
}

// OnSuspend registers a callback executed when the system enters sleep/suspend state.
func (w *Watcher) OnSuspend(fn func()) {
	if fn == nil {
		return
	}

	w.onSuspend.Mutate(func(list *[]func()) {
		*list = append(*list, fn)
	})
}

// OnResume registers a callback executed when the system resumes from sleep/suspend state.
func (w *Watcher) OnResume(fn func()) {
	if fn == nil {
		return
	}

	w.onResume.Mutate(func(list *[]func()) {
		*list = append(*list, fn)
	})
}

// Close terminates background power monitoring goroutines.
func (w *Watcher) Close() {
	w.cancel()
}

func (w *Watcher) monitorClockJumps(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			elapsed := now.Sub(lastTime)
			if elapsed > w.jumpThreshold {
				w.notifySuspendAndResume()
			}

			lastTime = now
		}
	}
}

func (w *Watcher) notifySuspendAndResume() {
	var (
		suspendFns []func()
		resumeFns  []func()
	)

	w.onSuspend.Read(func(list []func()) {
		suspendFns = slices.Clone(list)
	})

	w.onResume.Read(func(list []func()) {
		resumeFns = slices.Clone(list)
	})

	for _, fn := range suspendFns {
		fn()
	}

	for _, fn := range resumeFns {
		fn()
	}
}
