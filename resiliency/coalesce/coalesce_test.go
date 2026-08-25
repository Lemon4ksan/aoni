// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package coalesce_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/resiliency/coalesce"
)

func TestRequestCoalescing(t *testing.T) {
	t.Parallel()

	g := coalesce.NewGroup()

	var networkCalls int32

	handler := func() (*http.Response, error) {
		atomic.AddInt32(&networkCalls, 1)
		time.Sleep(50 * time.Millisecond) // Simulate slow network response

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"price": 95000}`))),
		}, nil
	}

	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()

			resp, err := g.Do(context.Background(), "GET:https://api.crypto.com/ticker/btc", handler)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, http.StatusOK, resp.StatusCode)

			body, _ := io.ReadAll(resp.Body)
			require.Equal(t, `{"price": 95000}`, string(body))
		}()
	}

	wg.Wait()

	// Exactly 1 network call should have occurred for all 50 concurrent requests!
	require.Equal(
		t,
		int32(1),
		atomic.LoadInt32(&networkCalls),
		"Singleflight should coalesce 50 parallel requests into 1",
	)
}

func TestTypedGroup(t *testing.T) {
	t.Parallel()

	g := coalesce.NewTypedGroup[string, int]()

	var (
		callCount atomic.Int64
		wg        sync.WaitGroup
	)

	for i := 0; i < 30; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			val, err := g.Do("user:42", func() (int, error) {
				callCount.Add(1)
				time.Sleep(30 * time.Millisecond)
				return 42, nil
			})
			require.NoError(t, err)
			require.Equal(t, 42, val)
		}()
	}

	wg.Wait()
	require.Equal(t, int64(1), callCount.Load())
}
