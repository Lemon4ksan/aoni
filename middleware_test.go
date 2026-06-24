// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRetryDoer struct {
	mu         sync.Mutex
	calls      int
	statusCode int
	header     http.Header
}

func (m *mockRetryDoer) Do(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	m.calls++
	status := m.statusCode
	m.mu.Unlock()

	if status == 0 {
		status = http.StatusOK
	}

	return &http.Response{
		StatusCode: status,
		Header:     m.header,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

func (m *mockRetryDoer) SetStatusCode(code int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.statusCode = code
}

func TestRetryMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("retry_on_failure_and_preserve_body", func(t *testing.T) {
		t.Parallel()

		m1 := &mockDoer{id: 1, statusCode: 502}
		rotator, err := NewProxyRotator(ProxyRotatorConfig{}, ClientWithProxy{Client: m1})
		require.NoError(t, err)
		t.Cleanup(func() { _ = rotator.Close() })

		opts := RetryOptions{
			MaxRetries: 3,
			Backoff:    5 * time.Millisecond,
		}

		retryMiddleware := RetryMiddleware(opts, ProxyRetryCondition(rotator))
		client := retryMiddleware(m1)

		bodyText := "test body"
		req, err := http.NewRequestWithContext(t.Context(), "POST", "http://test", strings.NewReader(bodyText))
		require.NoError(t, err)

		go func() {
			time.Sleep(10 * time.Millisecond)
			m1.SetStatusCode(200)
		}()

		resp, err := client.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		assert.GreaterOrEqual(t, m1.GetCalls(), 2)
		assert.Equal(t, 200, resp.StatusCode)
	})

	t.Run("max_retries_exceeded", func(t *testing.T) {
		t.Parallel()

		m1 := &mockDoer{id: 1, forceError: true}
		rotator, err := NewProxyRotator(ProxyRotatorConfig{}, ClientWithProxy{Client: m1})
		require.NoError(t, err)
		t.Cleanup(func() { _ = rotator.Close() })

		opts := RetryOptions{
			MaxRetries: 1,
			Backoff:    1 * time.Millisecond,
		}

		client := RetryMiddleware(opts, ProxyRetryCondition(rotator))(m1)
		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)

		_, err = client.Do(req)
		require.Error(t, err)
		assert.Equal(t, 2, m1.GetCalls())
	})

	t.Run("custom_condition", func(t *testing.T) {
		t.Parallel()

		var (
			calls int
			mu    sync.Mutex
		)

		m1 := DoerFunc(func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			calls++
			currentCalls := calls
			mu.Unlock()

			statusCode := http.StatusTooManyRequests
			if currentCalls > 2 {
				statusCode = http.StatusOK
			}

			return &http.Response{
				StatusCode: statusCode,
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		})

		opts := RetryOptions{
			MaxRetries: 2,
			Backoff:    1 * time.Microsecond,
		}

		condition := func(resp *http.Response, err error) bool {
			return resp != nil && resp.StatusCode == http.StatusTooManyRequests
		}

		retryMiddleware := RetryMiddleware(opts, condition)
		client := retryMiddleware(m1)
		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		assert.Equal(t, 3, calls)
		assert.Equal(t, 200, resp.StatusCode)
	})
}

func TestRecoveryMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("recover_from_panic_and_return_error", func(t *testing.T) {
		t.Parallel()

		panicDoer := DoerFunc(func(req *http.Request) (*http.Response, error) {
			panic("something went terribly wrong")
		})

		var panicVal any

		recovery := RecoveryMiddleware(func(r any) {
			panicVal = r
		})

		client := recovery(panicDoer)
		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "aoni: panic recovered during request execution: something went terribly wrong")
		assert.Equal(t, "something went terribly wrong", panicVal)
	})
}

func TestCircuitBreaker(t *testing.T) {
	t.Parallel()

	t.Run("trip_breaker_on_failures_and_allow_recovery", func(t *testing.T) {
		t.Parallel()

		m := &mockDoer{id: 1, statusCode: 500}
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			FailureThreshold: 0.5,
			MinRequests:      2,
			Cooldown:         15 * time.Millisecond,
			Window:           10 * time.Second,
		})

		client := CircuitBreakerMiddleware(cb, nil)(m)
		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://localhost", nil)
		require.NoError(t, err)

		_, err = client.Do(req)
		require.NoError(t, err)

		_, err = client.Do(req)
		require.NoError(t, err)

		_, err = client.Do(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "aoni: circuit breaker open for host localhost")

		time.Sleep(20 * time.Millisecond)

		m.SetStatusCode(200)

		resp, err := client.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestFallbackMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("fallback_on_transport_error", func(t *testing.T) {
		t.Parallel()

		m := &mockDoer{id: 1, forceError: true}
		fallback := FallbackJSON(http.StatusOK, map[string]string{"message": "fallback-data"})

		client := FallbackMiddleware()(m)

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://localhost", nil)
		require.NoError(t, err)

		req = req.WithContext(context.WithValue(req.Context(), fallbackCtxKey{}, fallback))

		resp, err := client.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.JSONEq(t, `{"message": "fallback-data"}`, string(body))
	})

	t.Run("fallback_on_custom_condition_5xx", func(t *testing.T) {
		t.Parallel()

		m := &mockDoer{id: 1, statusCode: 503}
		fallback := FallbackJSON(http.StatusOK, map[string]string{"message": "fallback-5xx"})

		isFailure := func(resp *http.Response, err error) bool {
			return err != nil || (resp != nil && resp.StatusCode >= 500)
		}
		client := FallbackMiddlewareEx(isFailure)(m)

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://localhost", nil)
		require.NoError(t, err)

		req = req.WithContext(context.WithValue(req.Context(), fallbackCtxKey{}, fallback))

		resp, err := client.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.JSONEq(t, `{"message": "fallback-5xx"}`, string(body))
	})
}

func TestRetryAfter(t *testing.T) {
	t.Parallel()

	t.Run("respect_retry_after_header", func(t *testing.T) {
		t.Parallel()

		m := &mockRetryDoer{
			statusCode: 429,
			header:     http.Header{"Retry-After": []string{"1"}},
		}

		opts := RetryOptions{
			MaxRetries: 1,
			Backoff:    5 * time.Millisecond,
		}

		condition := func(resp *http.Response, err error) bool {
			return resp != nil && resp.StatusCode == 429
		}

		client := RetryMiddleware(opts, condition)(m)
		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://localhost", nil)
		require.NoError(t, err)

		start := time.Now()

		go func() {
			time.Sleep(50 * time.Millisecond)
			m.SetStatusCode(200)
		}()

		resp, err := client.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		elapsed := time.Since(start)
		assert.GreaterOrEqual(t, elapsed, 900*time.Millisecond)
	})
}

func TestRetryMiddleware_JitterFull(t *testing.T) {
	t.Parallel()

	t.Run("retry_with_aws_full_jitter_strategy", func(t *testing.T) {
		t.Parallel()

		m := &mockRetryDoer{statusCode: 502}
		opts := RetryOptions{
			MaxRetries:     2,
			Backoff:        10 * time.Millisecond,
			JitterStrategy: JitterFull,
		}

		condition := func(resp *http.Response, err error) bool {
			return resp != nil && resp.StatusCode == 502
		}

		client := RetryMiddleware(opts, condition)(m)
		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://localhost", nil)
		require.NoError(t, err)

		go func() {
			time.Sleep(15 * time.Millisecond)
			m.SetStatusCode(200)
		}()

		resp, err := client.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		m.mu.Lock()
		calls := m.calls
		m.mu.Unlock()
		assert.GreaterOrEqual(t, calls, 2)
	})
}

func TestChaosMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("injects_latency", func(t *testing.T) {
		t.Parallel()

		m := &mockDoer{id: 1}
		cfg := ChaosConfig{
			LatencyMin: 15 * time.Millisecond,
			LatencyMax: 20 * time.Millisecond,
		}
		chaos := ChaosMiddleware(cfg)
		client := chaos(m)

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://localhost", nil)
		require.NoError(t, err)

		start := time.Now()
		resp, err := client.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		elapsed := time.Since(start)

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.GreaterOrEqual(t, elapsed, 15*time.Millisecond)
	})

	t.Run("injects_failure", func(t *testing.T) {
		t.Parallel()

		m := &mockDoer{id: 1}
		cfg := ChaosConfig{
			FailureRate: 1.0,
		}
		chaos := ChaosMiddleware(cfg)
		client := chaos(m)

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://localhost", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	})

	t.Run("no_failure_injected_when_failure_rate_is_zero", func(t *testing.T) {
		t.Parallel()

		m := &mockDoer{id: 1}
		cfg := ChaosConfig{
			FailureRate: 0.0,
		}
		chaos := ChaosMiddleware(cfg)
		client := chaos(m)

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://localhost", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}
