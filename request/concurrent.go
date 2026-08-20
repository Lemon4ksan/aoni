// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package request

import (
	"context"
	"errors"
	"iter"
	"sync"
	"sync/atomic"

	"github.com/lemon4ksan/foundation/generic"

	"github.com/lemon4ksan/aoni"
)

// DefaultConcurrencyLimit is the default worker pool size for large concurrent batch operations.
const DefaultConcurrencyLimit = 32

// ConcurrentResult encapsulates the execution outcome of a single request within a concurrent fan-out operation.
type ConcurrentResult[Resp any] struct {
	// Index represents the position of this result in the original input path slice.
	Index int
	// Value points to the unmarshaled response structure. It is nil when Err is non-nil.
	Value *Resp
	// Err holds any execution error encountered. It is nil on success.
	Err error
}

// Result converts the execution outcome into a Swift-inspired [generic.Result].
func (r ConcurrentResult[Resp]) Result() generic.Result[Resp] {
	if r.Err != nil {
		return generic.Failure[Resp](r.Err)
	}

	if r.Value == nil {
		return generic.Failure[Resp](errors.New("aoni/request: empty response value"))
	}

	return generic.Success(*r.Value)
}

// Optional converts the response value into a [generic.Optional].
func (r ConcurrentResult[Resp]) Optional() generic.Optional[Resp] {
	if r.Err == nil && r.Value != nil {
		return generic.Some(*r.Value)
	}

	return generic.None[Resp]()
}

// Unwrap returns the unpacked value and execution error.
func (r ConcurrentResult[Resp]) Unwrap() (Resp, error) {
	if r.Err != nil {
		var zero Resp
		return zero, r.Err
	}

	if r.Value == nil {
		var zero Resp
		return zero, errors.New("aoni/request: empty response value")
	}

	return *r.Value, nil
}

// Concurrent executes fn concurrently across paths using the provided [Requester], preserving original slice order.
//
// Invariants:
//   - c must be thread-safe for concurrent request execution across goroutines (such as [*aoni.Client]).
//
// Postconditions:
//   - The length of the returned results slice matches paths.
//   - Context cancellation on individual requests does not abort sibling operations.
func Concurrent[Resp any](
	ctx context.Context,
	c Requester,
	paths []string,
	fn func(ctx context.Context, c Requester, path string) (*Resp, error),
) []ConcurrentResult[Resp] {
	return ConcurrentWithLimit(ctx, c, paths, DefaultConcurrencyLimit, fn)
}

// ConcurrentWithLimit is like [Concurrent], but allows specifying an explicit maximum worker concurrency limit.
func ConcurrentWithLimit[Resp any](
	ctx context.Context,
	c Requester,
	paths []string,
	limit int,
	fn func(ctx context.Context, c Requester, path string) (*Resp, error),
) []ConcurrentResult[Resp] {
	return ConcurrentWithModsAndLimit(
		ctx,
		c,
		paths,
		nil,
		limit,
		func(ctx context.Context, c Requester, path string, _ ...aoni.RequestModifier) (*Resp, error) {
			return fn(ctx, c, path)
		},
	)
}

// ConcurrentWithMods is like [Concurrent], but applies per-request [aoni.RequestModifier] slices matching each path.
//
// Invariants:
//   - c must be thread-safe for concurrent request execution across goroutines (such as [*aoni.Client]).
func ConcurrentWithMods[Resp any](
	ctx context.Context,
	c Requester,
	paths []string,
	mods [][]aoni.RequestModifier,
	fn func(ctx context.Context, c Requester, path string, mods ...aoni.RequestModifier) (*Resp, error),
) []ConcurrentResult[Resp] {
	return ConcurrentWithModsAndLimit(ctx, c, paths, mods, DefaultConcurrencyLimit, fn)
}

// ConcurrentWithModsAndLimit executes fn concurrently across paths with an explicit worker concurrency limit
// and per-request modifiers, preserving original slice ordering in the returned results.
func ConcurrentWithModsAndLimit[Resp any](
	ctx context.Context,
	c Requester,
	paths []string,
	mods [][]aoni.RequestModifier,
	limit int,
	fn func(ctx context.Context, c Requester, path string, mods ...aoni.RequestModifier) (*Resp, error),
) []ConcurrentResult[Resp] {
	n := len(paths)
	if n == 0 {
		return nil
	}

	results := make([]ConcurrentResult[Resp], n)

	workerCount := limit
	if workerCount <= 0 || workerCount > n {
		workerCount = n
	}

	var (
		nextIdx atomic.Int64
		wg      sync.WaitGroup
	)

	wg.Add(workerCount)

	for range workerCount {
		go func() {
			defer wg.Done()

			for {
				idx := int(nextIdx.Add(1) - 1)
				if idx >= n {
					return
				}

				var reqMods []aoni.RequestModifier
				if idx < len(mods) {
					reqMods = mods[idx]
				}

				val, err := fn(ctx, c, paths[idx], reqMods...)
				results[idx] = ConcurrentResult[Resp]{Index: idx, Value: val, Err: err}
			}
		}()
	}

	wg.Wait()

	return results
}

// IterConcurrent executes fn concurrently across paths using a bounded worker pool and streams
// results over a Go 1.23+ range-over-func iterator as soon as each request completes.
// If the consumer breaks early from the loop, pending background requests are cancelled cleanly.
func IterConcurrent[Resp any](
	ctx context.Context,
	c Requester,
	paths []string,
	limit int,
	fn func(ctx context.Context, c Requester, path string) (*Resp, error),
) iter.Seq2[int, ConcurrentResult[Resp]] {
	return func(yield func(int, ConcurrentResult[Resp]) bool) {
		n := len(paths)
		if n == 0 {
			return
		}

		workerCount := limit
		if workerCount <= 0 || workerCount > n {
			workerCount = n
		}

		ctxIter, cancel := context.WithCancel(ctx)
		defer cancel()

		resCh := make(chan ConcurrentResult[Resp], workerCount)

		var (
			nextIdx atomic.Int64
			wg      sync.WaitGroup
		)

		wg.Add(workerCount)

		for range workerCount {
			go func() {
				defer wg.Done()

				for {
					if ctxIter.Err() != nil {
						return
					}

					idx := int(nextIdx.Add(1) - 1)
					if idx >= n {
						return
					}

					val, err := fn(ctxIter, c, paths[idx])
					res := ConcurrentResult[Resp]{Index: idx, Value: val, Err: err}

					select {
					case <-ctxIter.Done():
						return
					case resCh <- res:
					}
				}
			}()
		}

		go func() {
			wg.Wait()
			close(resCh)
		}()

		for res := range resCh {
			if !yield(res.Index, res) {
				return
			}
		}
	}
}
