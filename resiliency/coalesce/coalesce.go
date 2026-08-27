// Package coalesce implements concurrent in-flight request deduplication (Singleflight)
// preventing thundering herd spikes and redundant upstream network load.
//
// # Architectural Context: Singleflight Thundering-Herd Mitigation
//
// When hundreds of concurrent goroutines request the exact same cache-miss resource simultaneously,
// Singleflight coalesces all callers into a single in-flight remote request, broadcasting the resulting
// response body bytes to all waiting callers.
//
// # Example
//
//	resp, err := coalesce.DefaultGroup.Do(ctx, "resource-key", func() (*http.Response, error) {
//	    return client.Get(ctx, "/hot-data")
//	})
package coalesce

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/lemon4ksan/foundation/async/dedup"
)

type cachedResponse struct {
	statusCode int
	status     string
	proto      string
	header     http.Header
	body       []byte
}

// Group manages request coalescing and deduplication for concurrent in-flight operations.
type Group struct {
	group dedup.Group[string, *cachedResponse]
}

// NewGroup creates a new request deduplication [Group].
func NewGroup() *Group {
	return &Group{}
}

// DefaultGroup is the package-level shared singleflight group.
var DefaultGroup = NewGroup()

// Do executes and deduplicates fn by key. If a call with the same key is already in-flight,
// the caller waits and shares the response body without initiating a second network transaction.
func (g *Group) Do(ctx context.Context, key string, fn func() (*http.Response, error)) (*http.Response, error) {
	cached, err := g.group.Do(ctx, key, func(callCtx context.Context) (*cachedResponse, error) {
		resp, fnErr := fn()
		if fnErr != nil {
			return nil, fnErr
		}

		if resp == nil {
			return nil, errors.New("aoni/coalesce: nil response from handler")
		}

		// Buffer response body so all waiting goroutines receive independent copies
		bodyBytes, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if readErr != nil {
			return nil, readErr
		}

		return &cachedResponse{
			statusCode: resp.StatusCode,
			status:     resp.Status,
			proto:      resp.Proto,
			header:     resp.Header.Clone(),
			body:       bodyBytes,
		}, nil
	})
	if err != nil {
		return nil, err
	}

	return cached.toHTTPResponse(), nil
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
