// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package coalesce implements concurrent in-flight request deduplication (Singleflight)
// preventing thundering herd spikes and redundant upstream network queries.
package coalesce

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
)

// call represents an active or completed in-flight request.
type call struct {
	wg  sync.WaitGroup
	val *cachedResponse
	err error
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

		// Wait for in-flight call to finish or context cancellation
		waitCh := make(chan struct{})
		go func() {
			c.wg.Wait()
			close(waitCh)
		}()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-waitCh:
			if c.err != nil {
				return nil, c.err
			}

			return c.val.toHTTPResponse(), nil
		}
	}

	c := new(call)
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	resp, err := fn()
	if err != nil {
		c.err = err

		g.mu.Lock()
		delete(g.m, key)
		g.mu.Unlock()
		c.wg.Done()

		return nil, err
	}

	// Buffer response body so all waiting goroutines receive independent copies
	bodyBytes, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if readErr != nil {
		c.err = readErr

		g.mu.Lock()
		delete(g.m, key)
		g.mu.Unlock()
		c.wg.Done()

		return nil, readErr
	}

	c.val = &cachedResponse{
		statusCode: resp.StatusCode,
		status:     resp.Status,
		proto:      resp.Proto,
		header:     resp.Header.Clone(),
		body:       bodyBytes,
	}

	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
	c.wg.Done()

	return c.val.toHTTPResponse(), nil
}

func (cr *cachedResponse) toHTTPResponse() *http.Response {
	return &http.Response{
		StatusCode:    cr.statusCode,
		Status:        cr.status,
		Proto:         cr.proto,
		Header:        cr.header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(cr.body)),
		ContentLength: int64(len(cr.body)),
	}
}
