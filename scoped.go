// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"

	"github.com/lemon4ksan/foundation/borrow"
)

// FetchTo executes a request with method, path, and optional modifiers, unmarshaling the 2xx response into T.
func FetchTo[T any](
	ctx context.Context,
	c any,
	method, path string,
	mods ...RequestModifier,
) (T, *http.Response, error) {
	var (
		target T
		doer   HTTPRequester
	)

	if d, ok := c.(HTTPRequester); ok {
		doer = d
	} else if c == nil {
		doer = DefaultClient
	}

	resp, err := acquireRequestBuilder(doer).
		SetContext(ctx).
		SetResult(&target).
		Apply(mods...).
		Execute(method, path)

	return target, resp, err
}

// BatchFetchTo dispatches multiple requests concurrently and unmarshals each 2xx response payload into a slice of T.
func BatchFetchTo[T any](
	ctx context.Context,
	c any,
	method string,
	paths []string,
	mods ...RequestModifier,
) ([]T, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	results := make([]T, len(paths))

	type fetchResult struct {
		idx int
		err error
	}

	resCh := make(chan fetchResult, len(paths))

	for i, path := range paths {
		go func(idx int, p string) {
			val, resp, err := FetchTo[T](ctx, c, method, p, mods...)
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}

			if err == nil {
				results[idx] = val
			}

			resCh <- fetchResult{idx: idx, err: err}
		}(i, path)
	}

	var firstErr error
	for range paths {
		res := <-resCh
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
	}

	return results, firstErr
}

// BatchGetTo dispatches multiple GET requests concurrently and unmarshals each 2xx response payload into a slice of T.
func BatchGetTo[T any](
	ctx context.Context,
	c any,
	paths []string,
	mods ...RequestModifier,
) ([]T, error) {
	return BatchFetchTo[T](ctx, c, http.MethodGet, paths, mods...)
}

// FetchScoped executes a request with method, path, and optional modifiers, passing the decoded response
// into fn within an active [borrow.Scope].
func FetchScoped[T any](
	ctx context.Context,
	c any,
	method, path string,
	fn func(scope *borrow.Scope, val T, resp *http.Response) error,
	mods ...RequestModifier,
) error {
	var (
		target T
		doer   HTTPRequester
	)

	if d, ok := c.(HTTPRequester); ok {
		doer = d
	} else if c == nil {
		doer = DefaultClient
	}

	resp, err := acquireRequestBuilder(doer).
		SetContext(ctx).
		SetResult(&target).
		Apply(mods...).
		Execute(method, path)
	if err != nil {
		return err
	}

	if resp != nil && resp.Body != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
	}

	scope := borrow.AcquireScope()
	defer scope.Release()

	return fn(scope, target, resp)
}

// GetScoped dispatches a GET request and passes the decoded response T to fn within an active [borrow.Scope].
func GetScoped[T any](
	ctx context.Context,
	c any,
	path string,
	fn func(scope *borrow.Scope, val T, resp *http.Response) error,
	mods ...RequestModifier,
) error {
	return FetchScoped[T](ctx, c, http.MethodGet, path, fn, mods...)
}
