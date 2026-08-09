// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pipeline

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

// IdleEvictor satisfies connection pool idle cleanup requirements.
type IdleEvictor interface {
	CloseIdleConnections()
}

// Janitor manages connection pool idle connection cleanup and concurrency semaphore limits.
type Janitor struct {
	evictor      IdleEvictor
	maxInflight  int64
	currInflight atomic.Int64
	sem          chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// NewJanitor initializes a [Janitor] with background eviction interval and max inflight concurrency limit.
func NewJanitor(evictor IdleEvictor, evictInterval time.Duration, maxInflight int) *Janitor {
	ctx, cancel := context.WithCancel(context.Background())
	j := &Janitor{
		evictor:     evictor,
		maxInflight: int64(maxInflight),
		ctx:         ctx,
		cancel:      cancel,
	}

	if maxInflight > 0 {
		j.sem = make(chan struct{}, maxInflight)
	}

	if evictor != nil && evictInterval > 0 {
		j.wg.Add(1)

		go j.startEvictionLoop(evictInterval)
	}

	return j
}

// Acquire acquires a concurrency slot. Returns false if context is cancelled.
func (j *Janitor) Acquire(ctx context.Context) bool {
	if j == nil || j.sem == nil {
		if j != nil {
			j.currInflight.Add(1)
		}

		return true
	}

	select {
	case j.sem <- struct{}{}:
		j.currInflight.Add(1)
		return true
	case <-ctx.Done():
		return false
	}
}

// Release frees an acquired concurrency slot.
func (j *Janitor) Release() {
	if j == nil {
		return
	}

	j.currInflight.Add(-1)

	if j.sem != nil {
		select {
		case <-j.sem:
		default:
		}
	}
}

// Inflight returns current active inflight requests count.
func (j *Janitor) Inflight() int64 {
	if j == nil {
		return 0
	}

	return j.currInflight.Load()
}

// Stop shuts down the background janitor loop.
func (j *Janitor) Stop() {
	if j == nil {
		return
	}

	j.cancel()
	j.wg.Wait()
}

func (j *Janitor) startEvictionLoop(interval time.Duration) {
	defer j.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			safeCloseIdleConnections(j.evictor)
		case <-j.ctx.Done():
			return
		}
	}
}

func safeCloseIdleConnections(evictor IdleEvictor) {
	if evictor == nil {
		return
	}

	val := reflect.ValueOf(evictor)
	if val.Kind() == reflect.Pointer && val.IsNil() {
		return
	}

	evictor.CloseIdleConnections()
}
