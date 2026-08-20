// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package middleware

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil/netdial"
)

type mockDoer struct {
	mu         sync.RWMutex
	id         int
	calls      int
	forceError bool
	statusCode int
}

func (m *mockDoer) Do(req aoni.Request) (aoni.Response, error) {
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

	return aoni.NewStdResponse(&http.Response{StatusCode: statusCode, Body: http.NoBody}), err
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

type mockRetryDoer struct {
	mu         sync.Mutex
	calls      int
	statusCode int
	header     http.Header
}

func (m *mockRetryDoer) Do(_ aoni.Request) (aoni.Response, error) {
	m.mu.Lock()
	m.calls++
	status := m.statusCode
	m.mu.Unlock()

	if status == 0 {
		status = http.StatusOK
	}

	return aoni.NewStdResponse(&http.Response{
		StatusCode: status,
		Header:     m.header,
		Body:       io.NopCloser(strings.NewReader("")),
	}), nil
}

func (m *mockRetryDoer) SetStatusCode(code int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.statusCode = code
}

func TestChain_ExecutionOrder(t *testing.T) {
	t.Parallel()

	var executionOrder []string

	createMid := func(name string) aoni.Middleware {
		return func(next aoni.RequestDoer) aoni.RequestDoer {
			return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
				executionOrder = append(executionOrder, name+"_before")
				resp, err := next.Do(req)

				executionOrder = append(executionOrder, name+"_after")

				return resp, err
			})
		}
	}

	baseDoer := aoni.DoerFunc(func(_ aoni.Request) (aoni.Response, error) {
		executionOrder = append(executionOrder, "base_doer")
		return aoni.NewStdResponse(&http.Response{StatusCode: http.StatusOK}), nil
	})

	chained := Chain(baseDoer, createMid("m1"), createMid("m2"), createMid("m3"))

	httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
	require.NoError(t, err)

	_, err = chained.Do(aoni.NewStdRequest(httpReq))
	require.NoError(t, err)

	expectedOrder := []string{
		"m1_before", "m2_before", "m3_before",
		"base_doer",
		"m3_after", "m2_after", "m1_after",
	}

	assert.Equal(t, expectedOrder, executionOrder)
}

func TestRetryMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("custom_condition", func(t *testing.T) {
		t.Parallel()

		var (
			calls int
			mu    sync.Mutex
		)

		m1 := aoni.DoerFunc(func(_ aoni.Request) (aoni.Response, error) {
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

		opts := RetryOptions{
			MaxRetries: 2,
			Backoff:    1 * time.Microsecond,
		}

		condition := func(resp aoni.Response, _ error) bool {
			return resp != nil && resp.StatusCode() == http.StatusTooManyRequests
		}

		retryMiddleware := Retry(opts, condition)
		client := retryMiddleware(m1)
		httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://test", nil)
		require.NoError(t, err)

		resp, err := client.Do(aoni.NewStdRequest(httpReq))
		require.NoError(t, err)

		t.Cleanup(func() { _ = resp.Close() })

		assert.Equal(t, 3, calls)
		assert.Equal(t, 200, resp.StatusCode())
	})
}

func TestRecoveryMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("recover_from_panic_and_return_error", func(t *testing.T) {
		t.Parallel()

		panicDoer := aoni.DoerFunc(func(_ aoni.Request) (aoni.Response, error) {
			panic("something went terribly wrong")
		})

		var panicVal any

		recovery := Recover(func(r any) {
			panicVal = r
		})

		client := recovery(panicDoer)
		httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://test", nil)
		require.NoError(t, err)

		resp, err := client.Do(aoni.NewStdRequest(httpReq))
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "aoni: panic recovered during request execution: something went terribly wrong")
		assert.Equal(t, "something went terribly wrong", panicVal)
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

		condition := func(resp aoni.Response, _ error) bool {
			return resp != nil && resp.StatusCode() == 429
		}

		client := Retry(opts, condition)(m)
		httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
		require.NoError(t, err)

		start := time.Now()

		go func() {
			time.Sleep(50 * time.Millisecond)
			m.SetStatusCode(200)
		}()

		resp, err := client.Do(aoni.NewStdRequest(httpReq))
		require.NoError(t, err)

		t.Cleanup(func() { _ = resp.Close() })

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

		condition := func(resp aoni.Response, _ error) bool {
			return resp != nil && resp.StatusCode() == 502
		}

		client := Retry(opts, condition)(m)
		httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
		require.NoError(t, err)

		go func() {
			time.Sleep(15 * time.Millisecond)
			m.SetStatusCode(200)
		}()

		resp, err := client.Do(aoni.NewStdRequest(httpReq))
		require.NoError(t, err)

		t.Cleanup(func() { _ = resp.Close() })

		m.mu.Lock()
		calls := m.calls
		m.mu.Unlock()

		assert.GreaterOrEqual(t, calls, 2)
	})
}

func TestRetryMiddleware_FatalErrorNoRetry(t *testing.T) {
	t.Parallel()

	var attempts int

	mw := Retry(RetryOptions{MaxRetries: 3}, RetryOnErr())

	doer := mw(aoni.DoerFunc(func(_ aoni.Request) (aoni.Response, error) {
		attempts++
		return nil, netdial.ErrSSRFBlocked
	}))

	httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
	require.NoError(t, err)

	_, err = doer.Do(aoni.NewStdRequest(httpReq))
	assert.ErrorIs(t, err, netdial.ErrSSRFBlocked)
	assert.Equal(t, 1, attempts) // Instant abort on 1st attempt, zero retries
}

func TestRetryMiddleware_NegativeBackoff(t *testing.T) {
	t.Parallel()

	m := Retry(RetryOptions{
		MaxRetries: 1,
		Backoff:    -1 * time.Second,
	}, RetryOnErr())

	doer := m(aoni.DoerFunc(func(_ aoni.Request) (aoni.Response, error) {
		return nil, assert.AnError
	}))

	httpReq, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
	_, err := doer.Do(aoni.NewStdRequest(httpReq))
	assert.Error(t, err)
}

func TestParseRetryAfter_Overflow(t *testing.T) {
	t.Parallel()

	resp := aoni.NewStdResponse(&http.Response{
		Header: http.Header{"Retry-After": []string{"9999999999999999999999"}},
	})

	delay, has := parseRetryAfter(resp)
	assert.True(t, has)
	assert.Greater(t, delay, time.Duration(0))
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

		client := CircuitBreak(cb, nil)(m)
		httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
		require.NoError(t, err)

		req := aoni.NewStdRequest(httpReq)

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

		t.Cleanup(func() { _ = resp.Close() })

		assert.Equal(t, http.StatusOK, resp.StatusCode())
	})
}

func TestClient_CircuitBreaker(t *testing.T) {
	t.Parallel()

	cfg := CircuitBreakerConfig{
		FailureThreshold: 0.5,
		Cooldown:         50 * time.Millisecond,
		MinRequests:      2,
		Window:           5 * time.Second,
	}
	cb := NewCircuitBreaker(cfg)

	b := cb.getBreaker("example.com")
	assert.NotNil(t, b)

	_, err := b.Do(t.Context(), func(_ context.Context) (any, error) {
		return nil, errors.New("error")
	})
	require.Error(t, err)

	_, err = b.Do(t.Context(), func(_ context.Context) (any, error) {
		return nil, errors.New("error")
	})
	require.Error(t, err)

	_, err = b.Do(t.Context(), func(_ context.Context) (any, error) {
		return nil, nil
	})
	assert.ErrorContains(t, err, "circuit breaker is open")
}

func TestNewCircuitBreaker_NaN(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: math.NaN(),
	})

	assert.Equal(t, 0.5, cb.cfg.FailureThreshold)
}

func TestNewCircuitBreaker_Defaults(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker(CircuitBreakerConfig{})
	assert.Equal(t, 0.5, cb.cfg.FailureThreshold)
	assert.Equal(t, 5*time.Second, cb.cfg.Cooldown)
	assert.Equal(t, 5, cb.cfg.MinRequests)
	assert.Equal(t, 10*time.Second, cb.cfg.Window)
}

func TestFallbackMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("fallback_on_transport_error_json", func(t *testing.T) {
		t.Parallel()

		m := &mockDoer{id: 1, forceError: true}
		fallback := FallbackJSON(http.StatusOK, map[string]string{"message": "fallback-data"})

		client := Fallback()(m)

		httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
		require.NoError(t, err)

		req := aoni.NewStdRequest(httpReq)
		mod.WithFallback(fallback).Apply(req)

		resp, err := client.Do(req)
		require.NoError(t, err)

		t.Cleanup(func() { _ = resp.Close() })

		assert.Equal(t, http.StatusOK, resp.StatusCode())

		bodyBytes := resp.BodyBytes()
		assert.JSONEq(t, `{"message": "fallback-data"}`, string(bodyBytes))
	})

	t.Run("fallback_on_transport_error_string", func(t *testing.T) {
		t.Parallel()

		m := &mockDoer{id: 1, forceError: true}
		fallback := FallbackString(http.StatusGatewayTimeout, "text-fallback")

		client := Fallback()(m)

		httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
		require.NoError(t, err)

		req := aoni.NewStdRequest(httpReq)
		mod.WithFallback(fallback).Apply(req)

		resp, err := client.Do(req)
		require.NoError(t, err)

		t.Cleanup(func() { _ = resp.Close() })

		assert.Equal(t, http.StatusGatewayTimeout, resp.StatusCode())

		bodyBytes := resp.BodyBytes()
		assert.Equal(t, "text-fallback", string(bodyBytes))
		assert.Contains(t, resp.Header("Content-Type"), "text/plain")
	})

	t.Run("fallback_on_custom_condition_5xx", func(t *testing.T) {
		t.Parallel()

		m := &mockDoer{id: 1, statusCode: 503}
		fallback := FallbackJSON(http.StatusOK, map[string]string{"message": "fallback-5xx"})

		isFailure := func(resp aoni.Response, err error) bool {
			return err != nil || (resp != nil && resp.StatusCode() >= 500)
		}
		client := FallbackEx(isFailure)(m)

		httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
		require.NoError(t, err)

		req := aoni.NewStdRequest(httpReq)
		mod.WithFallback(fallback).Apply(req)

		resp, err := client.Do(req)
		require.NoError(t, err)

		t.Cleanup(func() { _ = resp.Close() })

		assert.Equal(t, http.StatusOK, resp.StatusCode())

		bodyBytes := resp.BodyBytes()
		assert.JSONEq(t, `{"message": "fallback-5xx"}`, string(bodyBytes))
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
		chaos := Chaos(cfg)
		client := chaos(m)

		httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
		require.NoError(t, err)

		start := time.Now()

		resp, err := client.Do(aoni.NewStdRequest(httpReq))
		require.NoError(t, err)

		t.Cleanup(func() { _ = resp.Close() })

		elapsed := time.Since(start)

		assert.Equal(t, http.StatusOK, resp.StatusCode())
		assert.GreaterOrEqual(t, elapsed, 15*time.Millisecond)
	})

	t.Run("injects_failure", func(t *testing.T) {
		t.Parallel()

		m := &mockDoer{id: 1}
		cfg := ChaosConfig{
			FailureRate: 1.0,
		}
		chaos := Chaos(cfg)
		client := chaos(m)

		httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
		require.NoError(t, err)

		resp, err := client.Do(aoni.NewStdRequest(httpReq))
		require.NoError(t, err)

		t.Cleanup(func() { _ = resp.Close() })

		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode())
	})

	t.Run("no_failure_injected_when_failure_rate_is_zero", func(t *testing.T) {
		t.Parallel()

		m := &mockDoer{id: 1}
		cfg := ChaosConfig{
			FailureRate: 0.0,
		}
		chaos := Chaos(cfg)
		client := chaos(m)

		httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
		require.NoError(t, err)

		resp, err := client.Do(aoni.NewStdRequest(httpReq))
		require.NoError(t, err)

		t.Cleanup(func() { _ = resp.Close() })

		assert.Equal(t, http.StatusOK, resp.StatusCode())
	})
}

func TestRateLimitMiddleware_ClampsNegative(t *testing.T) {
	t.Parallel()

	m := RateLimit(-5, -10)
	require.NotNil(t, m)

	doer := m(aoni.DoerFunc(func(_ aoni.Request) (aoni.Response, error) {
		return aoni.NewStdResponse(&http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}), nil
	}))

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost", nil)
	_, err := doer.Do(aoni.NewStdRequest(httpReq))
	assert.Error(t, err)
}

func TestSlidingWindowRateLimit(t *testing.T) {
	t.Parallel()

	mw := LimitEnforcer(NewSlidingWindowLimiter(3, 100*time.Millisecond))

	var calls int

	doer := mw(aoni.DoerFunc(func(_ aoni.Request) (aoni.Response, error) {
		calls++
		return aoni.NewStdResponse(&http.Response{StatusCode: http.StatusOK}), nil
	}))

	httpReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
	require.NoError(t, err)

	req := aoni.NewStdRequest(httpReq)
	start := time.Now()

	for range 5 {
		_, err := doer.Do(req)
		require.NoError(t, err)
	}

	elapsed := time.Since(start)

	assert.Equal(t, 5, calls)
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
}

func TestGRPCWebTimeoutAndMetadata(t *testing.T) {
	t.Parallel()

	mdMiddleware := GRPCMetadata(map[string]string{
		"x-grpc-test": "active",
	})
	timeoutMiddleware := GRPCWebTimeout(5 * time.Second)

	var (
		capturedTimeout string
		capturedCustom  string
	)

	doer := aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
		capturedTimeout = req.Header("grpc-timeout")
		capturedCustom = req.Header("x-grpc-test")

		return aoni.NewStdResponse(&http.Response{StatusCode: http.StatusOK}), nil
	})

	chained := Chain(doer, mdMiddleware, timeoutMiddleware)

	httpReq, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"http://localhost/grpc.Service/Method",
		nil,
	)
	require.NoError(t, err)

	_, err = chained.Do(aoni.NewStdRequest(httpReq))
	require.NoError(t, err)

	assert.Equal(t, "5S", capturedTimeout)
	assert.Equal(t, "active", capturedCustom)
}

func TestFormatGRPCTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		duration time.Duration
		expected string
	}{
		{duration: 0, expected: "0m"},
		{duration: -1 * time.Second, expected: "0m"},
		{duration: 500 * time.Millisecond, expected: "500m"},
		{duration: 5 * time.Second, expected: "5S"},
		{duration: 2 * time.Minute, expected: "2M"},
		{duration: 3 * time.Hour, expected: "3H"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, formatGRPCTimeout(tt.duration))
		})
	}
}

func TestMaskQueryParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "masks API key",
			input:    "https://api.steampowered.com/ISteamWebAPI?key=abc123&format=json",
			expected: "https://api.steampowered.com/ISteamWebAPI?format=json&key=%2A%2A%2A",
		},
		{
			name:     "masks access_token",
			input:    "https://api.steampowered.com/ISteamWebAPI?access_token=secret123&format=json",
			expected: "https://api.steampowered.com/ISteamWebAPI?access_token=%2A%2A%2A&format=json",
		},
		{
			name:     "masks token",
			input:    "https://api.steampowered.com/ISteamWebAPI?token=secret123&format=json",
			expected: "https://api.steampowered.com/ISteamWebAPI?format=json&token=%2A%2A%2A",
		},
		{
			name:     "preserves non-sensitive params",
			input:    "https://api.steampowered.com/ISteamWebAPI?format=json&language=english",
			expected: "https://api.steampowered.com/ISteamWebAPI?format=json&language=english",
		},
		{
			name:     "masks multiple sensitive params",
			input:    "https://api.steampowered.com/ISteamWebAPI?key=abc&access_token=xyz&format=json",
			expected: "https://api.steampowered.com/ISteamWebAPI?access_token=%2A%2A%2A&format=json&key=%2A%2A%2A",
		},
		{
			name:     "handles nil URL",
			input:    "",
			expected: "",
		},
		{
			name:     "handles URL without query params",
			input:    "https://api.steampowered.com/ISteamWebAPI",
			expected: "https://api.steampowered.com/ISteamWebAPI",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var u *url.URL
			if tt.input != "" {
				var err error

				u, err = url.Parse(tt.input)
				if err != nil {
					t.Fatalf("failed to parse URL: %v", err)
				}
			}

			result := maskQueryParams(u)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMaskQueryParams_DoesNotModifyOriginal(t *testing.T) {
	t.Parallel()

	original := "https://api.steampowered.com/ISteamWebAPI?key=abc123&format=json"

	u, err := url.Parse(original)
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}

	originalQuery := u.Query().Get("key")

	_ = maskQueryParams(u)

	if u.Query().Get("key") != originalQuery {
		t.Error("maskQueryParams should not modify the original URL")
	}
}
