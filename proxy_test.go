// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	return &http.Response{StatusCode: statusCode, Body: io.NopCloser(nil)}, err
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

type mockRoundTripper struct{}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestNewProxyClient(t *testing.T) {
	t.Parallel()

	t.Run("default_timeout", func(t *testing.T) {
		t.Parallel()

		cfg := ProxyConfig{}

		client, err := NewProxyClient(cfg)
		require.NoError(t, err)

		assert.Equal(t, 15*time.Second, client.Timeout)
	})

	t.Run("custom_config", func(t *testing.T) {
		t.Parallel()

		proxyAddr := "http://user:pass@1.2.3.4:8080"
		cfg := ProxyConfig{
			ProxyURL:           proxyAddr,
			Timeout:            5 * time.Second,
			InsecureSkipVerify: true,
		}

		client, err := NewProxyClient(cfg)
		require.NoError(t, err)

		assert.Equal(t, 5*time.Second, client.Timeout)

		transport := client.Transport.(*http.Transport)
		assert.True(t, transport.TLSClientConfig.InsecureSkipVerify)

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://google.com", nil)
		require.NoError(t, err)

		proxyURL, err := transport.Proxy(req)
		require.NoError(t, err)
		assert.Equal(t, proxyAddr, proxyURL.String())
	})

	t.Run("invalid_proxy_url", func(t *testing.T) {
		t.Parallel()

		cfg := ProxyConfig{
			ProxyURL: " ://invalid-url",
		}

		_, err := NewProxyClient(cfg)
		require.Error(t, err)
	})

	t.Run("no_proxy", func(t *testing.T) {
		t.Parallel()

		cfg := ProxyConfig{ProxyURL: ""}

		client, err := NewProxyClient(cfg)
		require.NoError(t, err)

		transport := client.Transport.(*http.Transport)
		if transport.Proxy != nil {
			req, err := http.NewRequestWithContext(t.Context(), "GET", "http://google.com", nil)
			require.NoError(t, err)

			p, err := transport.Proxy(req)
			require.NoError(t, err)
			assert.Nil(t, p)
		}
	})
}

func TestProxyRotator(t *testing.T) {
	t.Parallel()

	t.Run("empty_clients_error", func(t *testing.T) {
		t.Parallel()

		_, err := NewProxyRotator(ProxyRotatorConfig{})
		require.Error(t, err)
		assert.Equal(t, "aoni: proxy rotator requires at least one client", err.Error())
	})

	t.Run("round_robin_logic", func(t *testing.T) {
		t.Parallel()

		m1 := &mockDoer{id: 1}
		m2 := &mockDoer{id: 2}
		m3 := &mockDoer{id: 3}

		rotator, err := NewProxyRotator(ProxyRotatorConfig{}, m1, m2, m3)
		require.NoError(t, err)
		t.Cleanup(func() { _ = rotator.Close() })

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)

		for range 4 {
			_, err := rotator.Do(req)
			require.NoError(t, err)
		}

		assert.Equal(t, 1, m1.GetCalls())
		assert.Equal(t, 2, m2.GetCalls())
		assert.Equal(t, 1, m3.GetCalls())
	})

	t.Run("concurrency_safety", func(t *testing.T) {
		t.Parallel()

		count := 10
		clients := make([]HTTPDoer, count)

		mocks := make([]*mockDoer, count)
		for i := range count {
			mocks[i] = &mockDoer{id: i}
			clients[i] = mocks[i]
		}

		rotator, err := NewProxyRotator(ProxyRotatorConfig{}, clients...)
		require.NoError(t, err)
		t.Cleanup(func() { _ = rotator.Close() })

		var wg sync.WaitGroup

		iterations := 1000
		wg.Add(iterations)

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)

		for range iterations {
			go func() {
				defer wg.Done()

				_, _ = rotator.Do(req)
			}()
		}

		wg.Wait()

		totalCalls := 0
		for _, m := range mocks {
			totalCalls += m.GetCalls()
		}

		assert.Equal(t, iterations, totalCalls)
	})
}

func TestProxyRotator_HealthCheck(t *testing.T) {
	t.Parallel()

	m1 := &mockDoer{id: 1}
	m2 := &mockDoer{id: 2, forceError: true}

	cfg := ProxyRotatorConfig{
		MaxFails:   2,
		RetryAfter: 100 * time.Millisecond,
	}
	rotator, err := NewProxyRotator(cfg, m1, m2)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rotator.Close() })

	req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
	require.NoError(t, err)

	for range 5 {
		_, _ = rotator.Do(req)
	}

	for range 10 {
		resp, err := rotator.Do(req)
		if err != nil {
			continue
		}

		if resp != nil && m1.GetCalls() == 0 {
			t.Error("expected calls to go to m1 only")
		}
	}

	time.Sleep(150 * time.Millisecond)

	foundM2 := false
	for range 5 {
		_, _ = rotator.Do(req)

		if m2.GetCalls() > 2 {
			foundM2 = true
			break
		}
	}

	assert.True(t, foundM2, "m2 should have been retried after cooldown")
}

func TestProxyRotator_BackgroundHealthCheck(t *testing.T) {
	t.Parallel()

	m1 := &mockDoer{id: 1, forceError: true}

	cfg := ProxyRotatorConfig{
		MaxFails:            1,
		RetryAfter:          1 * time.Hour,
		HealthCheckURL:      "http://health",
		HealthCheckInterval: 50 * time.Millisecond,
	}

	rotator, err := NewProxyRotator(cfg, m1)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rotator.Close() })

	req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
	require.NoError(t, err)

	_, _ = rotator.Do(req)
	require.True(t, rotator.clients[0].unhealthy.Load(), "proxy should be unhealthy")

	m1.SetForceError(false)

	time.Sleep(150 * time.Millisecond)

	assert.False(t, rotator.clients[0].unhealthy.Load(), "proxy should be healthy after background check")
}

func TestProxyRotator_ContextCancellation(t *testing.T) {
	t.Parallel()

	m1 := &mockDoer{id: 1}
	rotator, err := NewProxyRotator(ProxyRotatorConfig{}, m1)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rotator.Close() })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://test", nil)
	require.NoError(t, err)
	_, err = rotator.Do(req)

	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, rotator.clients[0].unhealthy.Load(), "proxy should NOT be marked unhealthy on cancellation")
}

func TestProxyRotator_RetryOnProxyError(t *testing.T) {
	t.Parallel()

	m1 := &mockDoer{id: 1, statusCode: 407}
	m2 := &mockDoer{id: 2, statusCode: 200}

	rotator, err := NewProxyRotator(ProxyRotatorConfig{MaxFails: 1}, m1, m2)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rotator.Close() })

	req, err := http.NewRequestWithContext(t.Context(), "GET", "http://steam", nil)
	require.NoError(t, err)

	resp, err := rotator.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	req2, err := http.NewRequestWithContext(t.Context(), "GET", "http://steam", nil)
	require.NoError(t, err)

	_, err = rotator.Do(req2)
	require.NoError(t, err)

	assert.True(t, rotator.clients[0].unhealthy.Load(), "proxy 1 should be unhealthy after 407 error")
}

func TestProxyConfig_CustomTransport(t *testing.T) {
	t.Parallel()

	t.Run("custom_round_tripper", func(t *testing.T) {
		t.Parallel()

		mw := &mockRoundTripper{}
		cfg := ProxyConfig{
			Transport: mw,
		}
		client, err := NewProxyClient(cfg)
		require.NoError(t, err)
		assert.Equal(t, mw, client.Transport)
	})

	t.Run("custom_round_tripper_factory", func(t *testing.T) {
		t.Parallel()

		mw := &mockRoundTripper{}
		cfg := ProxyConfig{
			TransportFactory: func(c ProxyConfig) (http.RoundTripper, error) {
				return mw, nil
			},
		}
		client, err := NewProxyClient(cfg)
		require.NoError(t, err)
		assert.Equal(t, mw, client.Transport)
	})
}

func TestProxyRotator_StickySessionCleanup(t *testing.T) {
	t.Parallel()

	m1 := &mockDoer{id: 1}
	r, err := NewProxyRotator(ProxyRotatorConfig{}, m1)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	r.sessionTTL = 10 * time.Millisecond
	r.stickyKeyFunc = func(req *http.Request) string {
		return "session1"
	}

	req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
	require.NoError(t, err)
	_, err = r.Do(req)
	require.NoError(t, err)

	r.mu.RLock()
	entry, exists := r.sessions["session1"]
	r.mu.RUnlock()
	assert.True(t, exists)
	assert.Equal(t, 0, entry.clientIdx)

	time.Sleep(20 * time.Millisecond)

	r.mu.Lock()

	now := time.Now()
	for k, v := range r.sessions {
		if now.Sub(v.lastSeen) > r.sessionTTL {
			delete(r.sessions, k)
		}
	}

	r.mu.Unlock()

	r.mu.RLock()
	_, exists = r.sessions["session1"]
	r.mu.RUnlock()
	assert.False(t, exists, "session should be cleaned up after expiration")
}

func TestProxyRotator_Prewarm(t *testing.T) {
	t.Parallel()

	m1 := &mockDoer{id: 1}
	m2 := &mockDoer{id: 2}

	r, err := NewProxyRotator(ProxyRotatorConfig{}, m1, m2)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	r.Prewarm(t.Context(), "http://warmtarget.com")

	assert.Equal(t, 1, m1.GetCalls())
	assert.Equal(t, 1, m2.GetCalls())
}
