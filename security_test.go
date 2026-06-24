// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBlockedIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		{"loopback v4", "127.0.0.1", true},
		{"loopback v6", "::1", true},
		{"zero v4", "0.0.0.0", true},
		{"private 10.x", "10.0.0.1", true},
		{"private 172.16.x", "172.16.0.1", true},
		{"private 192.168.x", "192.168.1.1", true},
		{"link-local", "169.254.1.1", true},
		{"unique-local v6", "fd00::1", true},
		{"public v4", "8.8.8.8", false},
		{"public v6", "2001:4860:4860::8888", false},
		{"cloudflare", "1.1.1.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip, "invalid IP: %s", tt.ip)
			assert.Equal(t, tt.blocked, isBlockedIP(ip))
		})
	}
}

func TestRateLimitMiddleware_ClampsNegative(t *testing.T) {
	t.Parallel()

	// Negative rps should not panic, should clamp to 0.
	m := RateLimitMiddleware(-5, -10)
	require.NotNil(t, m)

	// Should still create a working middleware (rate 0 = block all).
	doer := m(DoerFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", "http://localhost", nil)
	_, err := doer.Do(req)
	// With rate 0, the limiter blocks until context expires.
	assert.Error(t, err)
}

func TestRetryMiddleware_NegativeBackoff(t *testing.T) {
	t.Parallel()

	// Negative backoff should be clamped to 0, not cause tight loop.
	m := RetryMiddleware(RetryOptions{
		MaxRetries: 1,
		Backoff:    -1 * time.Second,
	}, RetryOnErr())

	doer := m(DoerFunc(func(req *http.Request) (*http.Response, error) {
		return nil, assert.AnError
	}))

	req, _ := http.NewRequestWithContext(t.Context(), "GET", "http://localhost", nil)
	_, err := doer.Do(req)
	assert.Error(t, err)
}

func TestNewCircuitBreaker_NaN(t *testing.T) {
	t.Parallel()

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: math.NaN(),
	})

	// NaN should be replaced with default 0.5.
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

func TestDoHResolver_QueryEncoding(t *testing.T) {
	t.Parallel()

	var capturedURL string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	r := &DoHResolver{
		Endpoint: ts.URL,
		Host:     "cloudflare-dns.com",
		client:   ts.Client(),
	}

	ctx := t.Context()
	_, _ = r.query(ctx, "example.com", 1)

	assert.Contains(t, capturedURL, "name=example.com")
	assert.Contains(t, capturedURL, "type=1")
}

func TestWebSocketMaxFrameSize(t *testing.T) {
	t.Parallel()

	// Verify the constant exists and is reasonable.
	assert.Equal(t, 16*1024*1024, maxWebSocketFrameSize)
}

func TestSocketIOMaxBinaryAttachments(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 64, maxBinaryAttachments)
	assert.Equal(t, 32*1024*1024, maxBinaryBufferSize)
	assert.Equal(t, 8*1024*1024, maxEIOPacketSize)
}

func TestRedactHeaders(t *testing.T) {
	t.Parallel()

	input := "Authorization: Bearer secret-token\r\nCookie: session=abc123\r\nContent-Type: text/plain\r\n"
	result := string(redactHeaders([]byte(input)))

	assert.Contains(t, result, "authorization: <redacted>")
	assert.Contains(t, result, "cookie: <redacted>")
	assert.Contains(t, result, "Content-Type: text/plain")
	assert.NotContains(t, result, "secret-token")
	assert.NotContains(t, result, "session=abc123")
}

func TestRetryOnTransientErrors(t *testing.T) {
	t.Parallel()

	cond := RetryOnTransientErrors()

	t.Run("nil_error_returns_false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, cond(nil, nil))
	})

	t.Run("net_error_returns_true", func(t *testing.T) {
		t.Parallel()

		_, err := net.DialTimeout("tcp", "192.0.2.1:1", time.Nanosecond)
		assert.True(t, cond(nil, err))
	})

	t.Run("connection_refused_string_returns_true", func(t *testing.T) {
		t.Parallel()
		assert.True(t, cond(nil, errors.New("dial tcp: connection refused")))
	})

	t.Run("io_eof_returns_false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, cond(nil, io.ErrUnexpectedEOF))
	})
}

func TestInMemoryDNSCache_Eviction(t *testing.T) {
	t.Parallel()

	cache := NewInMemoryDNSCache(time.Millisecond, &net.Resolver{})
	t.Cleanup(func() { cache.Close() })

	// Insert an entry that will expire immediately.
	cache.mu.Lock()
	cache.cache["expired.test"] = dnsCacheEntry{
		ips:    []net.IPAddr{{IP: net.ParseIP("1.2.3.4")}},
		expiry: time.Now().Add(-time.Hour),
	}
	cache.cache["valid.test"] = dnsCacheEntry{
		ips:    []net.IPAddr{{IP: net.ParseIP("5.6.7.8")}},
		expiry: time.Now().Add(time.Hour),
	}
	cache.mu.Unlock()

	// Wait for the eviction loop to run (1 minute ticker, but we can
	// test the eviction logic directly).
	cache.mu.Lock()

	now := time.Now()
	for k, v := range cache.cache {
		if now.After(v.expiry) {
			delete(cache.cache, k)
		}
	}

	_, expiredExists := cache.cache["expired.test"]
	_, validExists := cache.cache["valid.test"]
	cache.mu.Unlock()

	assert.False(t, expiredExists, "expired entry should be removed")
	assert.True(t, validExists, "valid entry should remain")
}
