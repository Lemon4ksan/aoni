// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package request

import (
	"context"
	"sync"

	"github.com/lemon4ksan/aoni"
)

// ConcurrentResult encapsulates the execution outcome of a single request within a concurrent fan-out operation.
type ConcurrentResult[Resp any] struct {
	// Index represents the position of this result in the original input path slice.
	Index int
	// Value points to the unmarshaled response structure. It is nil when Err is non-nil.
	Value *Resp
	// Err holds any execution error encountered. It is nil on success.
	Err error
}

// Concurrent executes fn concurrently across paths using the provided [Requester], preserving original slice order.
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
	results := make([]ConcurrentResult[Resp], len(paths))

	var wg sync.WaitGroup
	wg.Add(len(paths))

	for i, path := range paths {
		go func(idx int, targetPath string) {
			defer wg.Done()

			val, err := fn(ctx, c, targetPath)
			results[idx] = ConcurrentResult[Resp]{Index: idx, Value: val, Err: err}
		}(i, path)
	}

	wg.Wait()

	return results
}

// ConcurrentWithMods is like [Concurrent], but applies per-request [aoni.RequestModifier] slices matching each path.
func ConcurrentWithMods[Resp any](
	ctx context.Context,
	c Requester,
	paths []string,
	mods [][]aoni.RequestModifier,
	fn func(ctx context.Context, c Requester, path string, mods ...aoni.RequestModifier) (*Resp, error),
) []ConcurrentResult[Resp] {
	results := make([]ConcurrentResult[Resp], len(paths))

	var wg sync.WaitGroup
	wg.Add(len(paths))

	for i, path := range paths {
		var reqMods []aoni.RequestModifier
		if i < len(mods) {
			reqMods = mods[i]
		}

		go func(idx int, targetPath string, targetMods []aoni.RequestModifier) {
			defer wg.Done()

			val, err := fn(ctx, c, targetPath, targetMods...)
			results[idx] = ConcurrentResult[Resp]{Index: idx, Value: val, Err: err}
		}(i, path, reqMods)
	}

	wg.Wait()

	return results
}
