// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/netutil/proxy"
)

type mockDoer struct {
	mu         sync.RWMutex
	id         int
	calls      int
	forceError bool
	statusCode int
}

func (m *mockDoer) Do(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	m.calls++
	forceError := m.forceError
	statusCode := m.statusCode
	m.mu.Unlock()

	if err := req.Context().Err(); err != nil {
		return nil, err
	}

	var err error
	if forceError {
		err = errors.New("forced error")
	}

	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	return &http.Response{StatusCode: statusCode, Body: http.NoBody}, err
}

func (m *mockDoer) SetStatusCode(code int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.statusCode = code
}

func (m *mockDoer) SetForceError(force bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.forceError = force
}

func (m *mockDoer) GetCalls() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.calls
}

func TestRetryMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("retry_on_failure_and_preserve_body", func(t *testing.T) {
		t.Parallel()

		m1 := &mockDoer{id: 1, statusCode: 502}
		rotator, err := proxy.NewRotator(proxy.RotatorConfig{}, proxy.WithClient{Client: m1})
		require.NoError(t, err)
		t.Cleanup(func() { _ = rotator.Close() })

		var callbackCalls int32

		opts := middleware.RetryOptions{
			MaxRetries: 3,
			Backoff:    5 * time.Millisecond,
			OnRetry: func(attempt uint32, err error, delay time.Duration) {
				atomic.AddInt32(&callbackCalls, 1)
			},
		}

		retryMiddleware := middleware.Retry(opts, proxy.RetryCondition(rotator))
		client := retryMiddleware(aoni.NewHTTPDoerAdapter(m1))

		bodyText := "test body"
		httpReq, err := http.NewRequestWithContext(t.Context(), "POST", "http://test", strings.NewReader(bodyText))
		require.NoError(t, err)

		go func() {
			time.Sleep(10 * time.Millisecond)
			m1.SetStatusCode(200)
		}()

		resp, err := client.Do(aoni.NewStdRequest(httpReq))
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Close() })

		assert.GreaterOrEqual(t, m1.GetCalls(), 2)
		assert.Equal(t, 200, resp.StatusCode())
		assert.GreaterOrEqual(t, atomic.LoadInt32(&callbackCalls), int32(1))
	})

	t.Run("max_retries_exceeded", func(t *testing.T) {
		t.Parallel()

		m1 := &mockDoer{id: 1, forceError: true}
		rotator, err := proxy.NewRotator(proxy.RotatorConfig{}, proxy.WithClient{Client: m1})
		require.NoError(t, err)
		t.Cleanup(func() { _ = rotator.Close() })

		opts := middleware.RetryOptions{
			MaxRetries: 1,
			Backoff:    1 * time.Millisecond,
		}

		client := middleware.Retry(opts, proxy.RetryCondition(rotator))(aoni.NewHTTPDoerAdapter(m1))
		httpReq, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)

		_, err = client.Do(aoni.NewStdRequest(httpReq))
		require.Error(t, err)
		assert.Equal(t, 2, m1.GetCalls())
	})

	t.Run("custom_condition", func(t *testing.T) {
		t.Parallel()

		var (
			calls int
			mu    sync.Mutex
		)

		m1 := aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			mu.Lock()
			calls++
			currentCalls := calls
			mu.Unlock()

			statusCode := http.StatusTooManyRequests
			if currentCalls > 2 {
				statusCode = http.StatusOK
			}

			return aoni.NewStdResponse(&http.Response{
				StatusCode: statusCode,
				Body:       io.NopCloser(strings.NewReader("")),
			}), nil
		})

		opts := middleware.RetryOptions{
			MaxRetries: 2,
			Backoff:    1 * time.Microsecond,
		}

		condition := func(resp aoni.Response, err error) bool {
			return resp != nil && resp.StatusCode() == http.StatusTooManyRequests
		}

		retryMiddleware := middleware.Retry(opts, condition)
		client := retryMiddleware(m1)
		httpReq, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)

		resp, err := client.Do(aoni.NewStdRequest(httpReq))
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Close() })

		assert.Equal(t, 3, calls)
		assert.Equal(t, 200, resp.StatusCode())
	})
}
