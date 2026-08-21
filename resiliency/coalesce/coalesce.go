// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package coalesce implements concurrent in-flight request deduplication (Singleflight)
// preventing thundering herd spikes and redundant upstream network queries.
package coalesce

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/lemon4ksan/foundation/async/dedup"
)

// call represents an active or completed in-flight request.
type call struct {
	done chan struct{}
	val  *cachedResponse
	err  error
}

type cachedResponse struct {
	statusCode int
	status     string
	proto      string
	header     http.Header
	body       []byte
}

// Group manages request coalescing and deduplication for concurrent in-flight operations.
type Group struct {
	mu sync.Mutex
	m  map[string]*call
}

// NewGroup creates a new request deduplication [Group].
func NewGroup() *Group {
	return &Group{
		m: make(map[string]*call),
	}
}

// DefaultGroup is the package-level shared singleflight group.
var DefaultGroup = NewGroup()

// Do executes and deduplicates fn by key. If a call with the same key is already in-flight,
// the caller waits and shares the response body without initiating a second network transaction.
func (g *Group) Do(ctx context.Context, key string, fn func() (*http.Response, error)) (*http.Response, error) {
	g.mu.Lock()
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.done:
			if c.err != nil {
				return nil, c.err
			}

			if c.val == nil {
				return nil, errors.New("aoni/coalesce: nil response from coalesced call")
			}

			return c.val.toHTTPResponse(), nil
		}
	}

	c := &call{
		done: make(chan struct{}),
	}
	g.m[key] = c
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		delete(g.m, key)
		g.mu.Unlock()
		close(c.done)
	}()

	resp, err := fn()
	if err != nil {
		c.err = err
		return nil, err
	}

	// Buffer response body so all waiting goroutines receive independent copies
	bodyBytes, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if readErr != nil {
		c.err = readErr
		return nil, readErr
	}

	c.val = &cachedResponse{
		statusCode: resp.StatusCode,
		status:     resp.Status,
		proto:      resp.Proto,
		header:     resp.Header.Clone(),
		body:       bodyBytes,
	}

	return c.val.toHTTPResponse(), nil
}

func (cr *cachedResponse) toHTTPResponse() *http.Response {
	if cr == nil {
		return nil
	}

	return &http.Response{
		StatusCode:    cr.statusCode,
		Status:        cr.status,
		Proto:         cr.proto,
		Header:        cr.header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(cr.body)),
		ContentLength: int64(len(cr.body)),
	}
}

// TypedGroup provides generic singleflight function deduplication for arbitrary keys and return types.
type TypedGroup[K comparable, V any] struct {
	group dedup.Group[K, V]
}

// NewTypedGroup creates a new generic Singleflight deduplicator.
func NewTypedGroup[K comparable, V any]() *TypedGroup[K, V] {
	return &TypedGroup[K, V]{}
}

// Do executes and deduplicates fn by key.
func (g *TypedGroup[K, V]) Do(key K, fn func() (V, error)) (V, error) {
	return g.group.Do(context.Background(), key, func(ctx context.Context) (V, error) {
		return fn()
	})
}
