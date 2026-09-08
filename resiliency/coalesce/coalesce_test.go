// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package coalesce_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/resiliency/coalesce"
)

type errReader struct{}

func (e *errReader) Read([]byte) (int, error) {
	return 0, errors.New("read fault")
}

func (e *errReader) Close() error {
	return nil
}

func TestRequestCoalescing_Success(t *testing.T) {
	t.Parallel()

	const goroutines = 30

	g := coalesce.NewGroup()

	var (
		networkCalls atomic.Int32
		entered      atomic.Int32
		wg           sync.WaitGroup
	)

	handler := func() (*http.Response, error) {
		networkCalls.Add(1)

		for entered.Load() < goroutines {
			time.Sleep(2 * time.Millisecond)
		}

		time.Sleep(10 * time.Millisecond)

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"price": 95000}`))),
		}, nil
	}

	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			entered.Add(1)

			resp, err := g.Do(context.Background(), "GET:https://api.crypto.com/ticker/btc", handler)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, http.StatusOK, resp.StatusCode)

			body, _ := io.ReadAll(resp.Body)
			require.Equal(t, `{"price": 95000}`, string(body))
		}()
	}

	wg.Wait()

	require.Equal(
		t,
		int32(1),
		networkCalls.Load(),
		"Singleflight should coalesce parallel requests into 1",
	)
}

func TestRequestCoalescing_Errors(t *testing.T) {
	t.Parallel()

	g := coalesce.NewGroup()

	t.Run("nil_response_returns_error", func(t *testing.T) {
		t.Parallel()

		_, err := g.Do(context.Background(), "key_nil", func() (*http.Response, error) {
			return nil, nil
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nil response from handler")
	})

	t.Run("handler_returns_error", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("upstream failure")
		_, err := g.Do(context.Background(), "key_err", func() (*http.Response, error) {
			return nil, expectedErr
		})
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("body_read_error", func(t *testing.T) {
		t.Parallel()

		_, err := g.Do(context.Background(), "key_read_err", func() (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       &errReader{},
			}, nil
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read fault")
	})

	t.Run("default_group_usage", func(t *testing.T) {
		t.Parallel()

		resp, err := coalesce.DefaultGroup.Do(context.Background(), "default_key", func() (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("default_ok"))),
			}, nil
		})
		require.NoError(t, err)

		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		assert.Equal(t, "default_ok", string(body))
	})
}

func TestTypedGroup(t *testing.T) {
	t.Parallel()

	t.Run("typed_group_coalescing", func(t *testing.T) {
		t.Parallel()

		g := coalesce.NewTypedGroup[string, int]()

		const numGoroutines = 20

		var (
			callCount atomic.Int64
			entered   atomic.Int64
			wg        sync.WaitGroup
		)

		for range numGoroutines {
			wg.Add(1)

			go func() {
				defer wg.Done()

				entered.Add(1)

				val, err := g.Do("user:42", func() (int, error) {
					callCount.Add(1)
					// Wait until all sibling goroutines have reached g.Do
					for entered.Load() < numGoroutines {
						time.Sleep(2 * time.Millisecond)
					}

					time.Sleep(10 * time.Millisecond)

					return 42, nil
				})
				require.NoError(t, err)
				require.Equal(t, 42, val)
			}()
		}

		wg.Wait()
		require.Equal(t, int64(1), callCount.Load())
	})

	t.Run("typed_group_error_propagation", func(t *testing.T) {
		t.Parallel()

		g := coalesce.NewTypedGroup[string, string]()
		expectedErr := errors.New("typed error")

		_, err := g.Do("err_key", func() (string, error) {
			return "", expectedErr
		})
		require.ErrorIs(t, err, expectedErr)
	})
}
