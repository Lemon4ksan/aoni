// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"sync"
)

// ConcurrentResult holds the outcome of a single request in a concurrent fan-out.
type ConcurrentResult[Resp any] struct {
	// Index is the position of this result in the original input slice.
	Index int
	// Value is the decoded response payload. It is nil when Err is non-nil.
	Value *Resp
	// Err is any error returned by the request. It is nil on success.
	Err error
}

// Concurrent executes fn for each path in paths concurrently using the provided
// Requester, collecting results preserving the original order of paths.
// The results slice always has the same length as paths.
// Each request inherits the parent context; individual cancellations do not
// affect sibling requests.
//
// Example: fan-out GET requests.
//
//	results := aoni.Concurrent(ctx, client, paths,
//	    func(ctx context.Context, c aoni.Requester, path string) (*MyType, error) {
//	        return aoni.GetTo[MyType](ctx, c, path)
//	    })
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
		go func() {
			defer wg.Done()

			val, err := fn(ctx, c, path)
			results[i] = ConcurrentResult[Resp]{Index: i, Value: val, Err: err}
		}()
	}

	wg.Wait()

	return results
}

// ConcurrentWithMods is like [Concurrent] but passes per-request modifiers
// alongside each path. The mods slice must have the same length as paths,
// or be nil/empty in which case no per-request modifiers are applied.
func ConcurrentWithMods[Resp any](
	ctx context.Context,
	c Requester,
	paths []string,
	mods [][]RequestModifier,
	fn func(ctx context.Context, c Requester, path string, mods ...RequestModifier) (*Resp, error),
) []ConcurrentResult[Resp] {
	results := make([]ConcurrentResult[Resp], len(paths))

	var wg sync.WaitGroup
	wg.Add(len(paths))

	for i, path := range paths {
		var reqMods []RequestModifier
		if i < len(mods) {
			reqMods = mods[i]
		}

		go func() {
			defer wg.Done()

			val, err := fn(ctx, c, path, reqMods...)
			results[i] = ConcurrentResult[Resp]{Index: i, Value: val, Err: err}
		}()
	}

	wg.Wait()

	return results
}
