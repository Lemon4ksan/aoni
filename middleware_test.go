// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
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

		var callbackCalls int32

		opts := RetryOptions{
			MaxRetries: 3,
			Backoff:    5 * time.Millisecond,
			OnRetry: func(attempt uint32, err error, delay time.Duration) {
				atomic.AddInt32(&callbackCalls, 1)
			},
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
		assert.GreaterOrEqual(t, atomic.LoadInt32(&callbackCalls), int32(1))
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

func TestRetryCondition_Combinators(t *testing.T) {
	t.Parallel()

	condTrue := func(resp *http.Response, err error) bool { return true }
	condFalse := func(resp *http.Response, err error) bool { return false }

	t.Run("or_combinator", func(t *testing.T) {
		t.Parallel()
		assert.True(t, Or(condTrue, condFalse)(nil, nil))
		assert.False(t, Or(condFalse, condFalse)(nil, nil))
	})

	t.Run("and_combinator", func(t *testing.T) {
		t.Parallel()
		assert.True(t, And(condTrue, condTrue)(nil, nil))
		assert.False(t, And(condTrue, condFalse)(nil, nil))
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

	t.Run("fallback_on_transport_error_json", func(t *testing.T) {
		t.Parallel()

		m := &mockDoer{id: 1, forceError: true}
		fallback := FallbackJSON(http.StatusOK, map[string]string{"message": "fallback-data"})

		client := FallbackMiddleware()(m)

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://localhost", nil)
		require.NoError(t, err)

		WithFallback(fallback)(req)

		resp, err := client.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.JSONEq(t, `{"message": "fallback-data"}`, string(body))
	})

	t.Run("fallback_on_transport_error_string", func(t *testing.T) {
		t.Parallel()

		m := &mockDoer{id: 1, forceError: true}
		fallback := FallbackString(http.StatusGatewayTimeout, "text-fallback")

		client := FallbackMiddleware()(m)

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://localhost", nil)
		require.NoError(t, err)

		WithFallback(fallback)(req)

		resp, err := client.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusGatewayTimeout, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "text-fallback", string(body))
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/plain")
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

		WithFallback(fallback)(req)

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
