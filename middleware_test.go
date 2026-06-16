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

func TestRetryMiddleware(t *testing.T) {
	t.Run("Retry on failure and preserve body", func(t *testing.T) {
		m1 := &mockDoer{id: 1, statusCode: 502}
		rotator, _ := NewProxyRotator(ProxyRotatorConfig{}, m1)

		opts := RetryOptions{
			MaxRetries: 3,
			Backoff:    5 * time.Millisecond,
		}

		retryMiddleware := RetryMiddleware(opts, ProxyRetryCondition(rotator))
		client := retryMiddleware(m1)

		bodyText := "test body"
		req, _ := http.NewRequest("POST", "http://test", strings.NewReader(bodyText))

		go func() {
			time.Sleep(10 * time.Millisecond)

			m1.SetStatusCode(200)
		}()

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("expected success after retry, got %v", err)
		}
		defer resp.Body.Close()

		if m1.GetCalls() < 2 {
			t.Errorf("expected at least 2 calls, got %d", m1.GetCalls())
		}

		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Max retries exceeded", func(t *testing.T) {
		m1 := &mockDoer{id: 1, forceError: true}
		rotator, _ := NewProxyRotator(ProxyRotatorConfig{}, m1)

		opts := RetryOptions{
			MaxRetries: 1,
			Backoff:    1 * time.Millisecond,
		}

		client := RetryMiddleware(opts, ProxyRetryCondition(rotator))(m1)
		req, _ := http.NewRequest("GET", "http://test", nil)

		_, err := client.Do(req)
		if err == nil {
			t.Fatal("expected error after max retries, got nil")
		}

		if m1.GetCalls() != 2 { // Initial + 1 retry
			t.Errorf("expected 2 calls, got %d", m1.GetCalls())
		}
	})

	t.Run("Custom condition", func(t *testing.T) {
		m1 := &mockDoer{id: 1, statusCode: 429}

		opts := RetryOptions{
			MaxRetries: 2,
			Backoff:    5 * time.Millisecond,
		}

		// Custom condition: retry on 429
		condition := func(resp *http.Response, err error) bool {
			return resp != nil && resp.StatusCode == http.StatusTooManyRequests
		}

		retryMiddleware := RetryMiddleware(opts, condition)
		client := retryMiddleware(m1)
		req, _ := http.NewRequest("GET", "http://test", nil)

		go func() {
			time.Sleep(8 * time.Millisecond)
			m1.SetStatusCode(200)
		}()

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("expected success after retry, got %v", err)
		}
		defer resp.Body.Close()

		if m1.GetCalls() < 2 {
			t.Errorf("expected at least 2 calls, got %d", m1.GetCalls())
		}

		if resp.StatusCode != 200 {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})
}

func TestRecoveryMiddleware(t *testing.T) {
	t.Run("Recover from panic and return error", func(t *testing.T) {
		panicDoer := DoerFunc(func(req *http.Request) (*http.Response, error) {
			panic("something went terribly wrong")
		})

		var panicVal any

		recovery := RecoveryMiddleware(func(r any) {
			panicVal = r
		})

		client := recovery(panicDoer)
		req, _ := http.NewRequest("GET", "http://test", nil)

		resp, err := client.Do(req)
		if err == nil {
			t.Fatal("expected error from panic, got nil")
		}

		if resp != nil {
			t.Error("expected nil response on panic")
		}

		if !strings.Contains(
			err.Error(),
			"aoni: panic recovered during request execution: something went terribly wrong",
		) {
			t.Errorf("unexpected error message: %v", err)
		}

		if panicVal != "something went terribly wrong" {
			t.Errorf("expected panic value 'something went terribly wrong', got %v", panicVal)
		}
	})
}

func TestCircuitBreaker(t *testing.T) {
	t.Run("Trip breaker on failures and allow recovery", func(t *testing.T) {
		m := &mockDoer{id: 1, statusCode: 500}
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			FailureThreshold: 2,
			SuccessThreshold: 1,
			Cooldown:         15 * time.Millisecond,
		})

		client := CircuitBreakerMiddleware(cb, nil)(m)
		req, _ := http.NewRequest("GET", "http://localhost", nil)

		// First failure
		_, err := client.Do(req)
		require.NoError(t, err) // m returns 500 but no transport error
		assert.Equal(t, uint32(1), cb.getCircuit("localhost").failCount)

		// Second failure (should trip)
		_, err = client.Do(req)
		require.NoError(t, err)
		assert.Equal(t, StateOpen, cb.getCircuit("localhost").state)

		// Third request should be blocked immediately by circuit breaker
		_, err = client.Do(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "aoni: circuit breaker open for host localhost")

		// Wait for cooldown
		time.Sleep(20 * time.Millisecond)

		// Should transition to Half-Open and allow request
		m.SetStatusCode(200) // make it succeed

		resp, err := client.Do(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Should transition back to Closed
		assert.Equal(t, StateClosed, cb.getCircuit("localhost").state)
		assert.Equal(t, uint32(0), cb.getCircuit("localhost").failCount)
	})
}

func TestFallbackMiddleware(t *testing.T) {
	t.Run("Fallback on transport error", func(t *testing.T) {
		m := &mockDoer{id: 1, forceError: true}
		fallback := FallbackJSON(http.StatusOK, map[string]string{"message": "fallback-data"})

		client := FallbackMiddleware()(m)

		req, _ := http.NewRequest("GET", "http://localhost", nil)
		req = req.WithContext(context.WithValue(req.Context(), fallbackCtxKey{}, fallback))

		resp, err := client.Do(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		assert.JSONEq(t, `{"message": "fallback-data"}`, string(body))
	})

	t.Run("Fallback on custom condition (5xx)", func(t *testing.T) {
		m := &mockDoer{id: 1, statusCode: 503}
		fallback := FallbackJSON(http.StatusOK, map[string]string{"message": "fallback-5xx"})

		isFailure := func(resp *http.Response, err error) bool {
			return err != nil || (resp != nil && resp.StatusCode >= 500)
		}
		client := FallbackMiddlewareEx(isFailure)(m)

		req, _ := http.NewRequest("GET", "http://localhost", nil)
		req = req.WithContext(context.WithValue(req.Context(), fallbackCtxKey{}, fallback))

		resp, err := client.Do(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		assert.JSONEq(t, `{"message": "fallback-5xx"}`, string(body))
	})
}

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

func TestRetryAfter(t *testing.T) {
	t.Run("Respect Retry-After header", func(t *testing.T) {
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
		req, _ := http.NewRequest("GET", "http://localhost", nil)

		start := time.Now()

		go func() {
			time.Sleep(50 * time.Millisecond)
			m.SetStatusCode(200)
		}()

		resp, err := client.Do(req)
		require.NoError(t, err)

		_ = resp.Body.Close()

		elapsed := time.Since(start)
		assert.GreaterOrEqual(t, elapsed, 900*time.Millisecond)
	})
}

func TestRetryMiddleware_JitterFull(t *testing.T) {
	t.Run("Retry with AWS Full Jitter strategy", func(t *testing.T) {
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
		req, _ := http.NewRequest("GET", "http://localhost", nil)

		go func() {
			time.Sleep(15 * time.Millisecond)
			m.SetStatusCode(200)
		}()

		resp, err := client.Do(req)
		require.NoError(t, err)

		_ = resp.Body.Close()

		m.mu.Lock()
		calls := m.calls
		m.mu.Unlock()
		assert.GreaterOrEqual(t, calls, 2)
	})
}

func TestChaosMiddleware(t *testing.T) {
	t.Run("Injects latency", func(t *testing.T) {
		m := &mockDoer{id: 1}
		cfg := ChaosConfig{
			LatencyMin: 15 * time.Millisecond,
			LatencyMax: 20 * time.Millisecond,
		}
		chaos := ChaosMiddleware(cfg)
		client := chaos(m)

		req, _ := http.NewRequest("GET", "http://localhost", nil)
		start := time.Now()
		resp, err := client.Do(req)
		elapsed := time.Since(start)

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.GreaterOrEqual(t, elapsed, 15*time.Millisecond)
	})

	t.Run("Injects failure", func(t *testing.T) {
		m := &mockDoer{id: 1}
		cfg := ChaosConfig{
			FailureRate: 1.0, // 100% failure rate
		}
		chaos := ChaosMiddleware(cfg)
		client := chaos(m)

		req, _ := http.NewRequest("GET", "http://localhost", nil)
		resp, err := client.Do(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	})

	t.Run("No failure injected when FailureRate is 0.0", func(t *testing.T) {
		m := &mockDoer{id: 1}
		cfg := ChaosConfig{
			FailureRate: 0.0,
		}
		chaos := ChaosMiddleware(cfg)
		client := chaos(m)

		req, _ := http.NewRequest("GET", "http://localhost", nil)
		resp, err := client.Do(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestAdaptiveLimiter(t *testing.T) {
	t.Run("Acquire blocks when limit exceeded", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(2)
		ctx := context.Background()

		err := limiter.Acquire(ctx)
		require.NoError(t, err)

		err = limiter.Acquire(ctx)
		require.NoError(t, err)

		acquiredCh := make(chan struct{})
		go func() {
			err := limiter.Acquire(ctx)
			if err == nil {
				close(acquiredCh)
			}
		}()

		select {
		case <-acquiredCh:
			t.Fatal("should have blocked")
		case <-time.After(10 * time.Millisecond):
			// Successfully blocked
		}

		limiter.Release(10 * time.Millisecond)

		select {
		case <-acquiredCh:
			// Woken up successfully!
		case <-time.After(100 * time.Millisecond):
			t.Fatal("waiter was not woken up")
		}
	})

	t.Run("Adjusts limit dynamically based on RTT", func(t *testing.T) {
		limiter := NewAdaptiveLimiter(10)
		limiter.minLimit = 2
		limiter.maxLimit = 20

		limiter.Release(10 * time.Millisecond)

		for i := 0; i < 20; i++ {
			limiter.Release(50 * time.Millisecond)
		}

		assert.Less(t, limiter.Limit(), 10.0)

		oldLimit := limiter.Limit()
		for i := 0; i < 20; i++ {
			limiter.Release(10 * time.Millisecond)
		}

		assert.Greater(t, limiter.Limit(), oldLimit)
	})
}
