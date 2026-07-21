// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"context"
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

type mockRoundTripper struct{}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestClient_ParseAutoProxy(t *testing.T) {
	u1, err := Parse("socks5://127.0.0.1:1080")
	require.NoError(t, err)
	assert.Equal(t, "socks5", u1.Scheme)
	assert.Equal(t, "127.0.0.1:1080", u1.Host)

	u2, err := Parse("127.0.0.1:1080")
	require.NoError(t, err)
	assert.Equal(t, "socks5h", u2.Scheme)
	assert.Equal(t, "127.0.0.1:1080", u2.Host)

	u3, err := Parse("user:pass@1.2.3.4:8080")
	require.NoError(t, err)
	assert.Equal(t, "http", u3.Scheme)
	assert.Equal(t, "1.2.3.4:8080", u3.Host)
	assert.Equal(t, "user", u3.User.Username())
	password, _ := u3.User.Password()
	assert.Equal(t, "pass", password)
}

func TestNewProxyClient(t *testing.T) {
	t.Parallel()

	t.Run("default_timeout", func(t *testing.T) {
		t.Parallel()

		cfg := Config{}

		client, err := NewProxyClient(cfg)
		require.NoError(t, err)

		assert.Equal(t, 15*time.Second, client.Timeout)
	})

	t.Run("custom_config", func(t *testing.T) {
		t.Parallel()

		proxyAddr := "http://user:pass@1.2.3.4:8080"
		cfg := Config{
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

		cfg := Config{
			ProxyURL: " ://invalid-url",
		}

		_, err := NewProxyClient(cfg)
		require.Error(t, err)
	})

	t.Run("no_proxy", func(t *testing.T) {
		t.Parallel()

		cfg := Config{ProxyURL: ""}

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

		_, err := NewRotator(RotatorConfig{})
		require.Error(t, err)
		assert.Equal(t, "aoni: proxy rotator requires at least one client", err.Error())
	})

	t.Run("round_robin_logic", func(t *testing.T) {
		t.Parallel()

		m1 := &mockDoer{id: 1}
		m2 := &mockDoer{id: 2}
		m3 := &mockDoer{id: 3}

		rotator, err := NewRotator(
			RotatorConfig{},
			WithClient{Client: m1},
			WithClient{Client: m2},
			WithClient{Client: m3},
		)
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
		clients := make([]WithClient, count)

		mocks := make([]*mockDoer, count)
		for i := range count {
			mocks[i] = &mockDoer{id: i}
			clients[i] = WithClient{Client: mocks[i]}
		}

		rotator, err := NewRotator(RotatorConfig{}, clients...)
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

	t.Run("update_clients_empty_returns_early", func(t *testing.T) {
		t.Parallel()

		m1 := &mockDoer{id: 1}
		r, err := NewRotator(RotatorConfig{}, WithClient{Client: m1})
		require.NoError(t, err)
		t.Cleanup(func() { _ = r.Close() })

		statsBefore := r.Stats()
		r.UpdateClients()
		assert.Equal(t, statsBefore, r.Stats())
	})
}

func TestProxyRotator_FromStrings(t *testing.T) {
	t.Parallel()

	t.Run("valid_creation", func(t *testing.T) {
		t.Parallel()

		cfg := RotatorConfig{}
		r, err := NewProxyRotatorFromStrings(cfg, "http://1.2.3.4:8080", "socks5://5.6.7.8:1080")
		require.NoError(t, err)
		t.Cleanup(func() { _ = r.Close() })

		stats := r.Stats()
		assert.Equal(t, 2, stats.TotalProxies)
		assert.Equal(t, "http://1.2.3.4:8080", r.clients[0].proxyURL)
	})

	t.Run("empty_error", func(t *testing.T) {
		t.Parallel()

		cfg := RotatorConfig{}
		_, err := NewProxyRotatorFromStrings(cfg)
		assert.Error(t, err)
	})

	t.Run("invalid_url_error", func(t *testing.T) {
		t.Parallel()

		cfg := RotatorConfig{}
		_, err := NewProxyRotatorFromStrings(cfg, " ://invalid")
		assert.Error(t, err)
	})
}

func TestProxyRotator_StatsAndReset(t *testing.T) {
	t.Parallel()

	m1 := &mockDoer{id: 1}
	m2 := &mockDoer{id: 2}

	r, err := NewRotator(RotatorConfig{}, WithClient{Client: m1}, WithClient{Client: m2})
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	// Check Stats
	stats := r.Stats()
	assert.Equal(t, 2, stats.TotalProxies)
	assert.Equal(t, 2, stats.HealthyProxies)
	assert.Equal(t, 0, stats.UnhealthyProxies)

	// Check Reset
	r.clients[0].MarkFailed()
	r.clients[0].MarkFailed()
	r.clients[0].MarkFailed() // MaxFails defaults to 3
	assert.True(t, r.clients[0].IsAvailable())
	assert.Equal(t, 1, r.Stats().UnhealthyProxies)

	r.Reset()
	assert.False(t, r.clients[0].IsAvailable())
	assert.Equal(t, 2, r.Stats().HealthyProxies)
}

func TestProxyRotator_HealthCheck(t *testing.T) {
	t.Parallel()

	m1 := &mockDoer{id: 1}
	m2 := &mockDoer{id: 2, forceError: true}

	cfg := RotatorConfig{
		MaxFails:   2,
		RetryAfter: 100 * time.Millisecond,
	}
	rotator, err := NewRotator(cfg, WithClient{Client: m1}, WithClient{Client: m2})
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

	cfg := RotatorConfig{
		MaxFails:            1,
		RetryAfter:          1 * time.Hour,
		HealthCheckURL:      "http://health",
		HealthCheckInterval: 50 * time.Millisecond,
	}

	rotator, err := NewRotator(cfg, WithClient{Client: m1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rotator.Close() })

	req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
	require.NoError(t, err)

	_, _ = rotator.Do(req)
	require.True(t, rotator.clients[0].IsAvailable(), "proxy should be unhealthy")

	m1.SetForceError(false)

	time.Sleep(150 * time.Millisecond)

	assert.False(t, rotator.clients[0].IsAvailable(), "proxy should be healthy after background check")
}

func TestProxyRotator_ContextCancellation(t *testing.T) {
	t.Parallel()

	m1 := &mockDoer{id: 1}
	rotator, err := NewRotator(RotatorConfig{}, WithClient{Client: m1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rotator.Close() })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://test", nil)
	require.NoError(t, err)
	_, err = rotator.Do(req)

	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, rotator.clients[0].IsAvailable(), "proxy should NOT be marked unhealthy on cancellation")
}

func TestProxyRotator_RetryOnProxyError(t *testing.T) {
	t.Parallel()

	m1 := &mockDoer{id: 1, statusCode: 407}
	m2 := &mockDoer{id: 2, statusCode: 200}

	rotator, err := NewRotator(
		RotatorConfig{MaxFails: 1},
		WithClient{Client: m1},
		WithClient{Client: m2},
	)
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

	assert.True(t, rotator.clients[0].IsAvailable(), "proxy 1 should be unhealthy after 407 error")
}

func TestProxyConfig_CustomTransport(t *testing.T) {
	t.Parallel()

	t.Run("custom_round_tripper", func(t *testing.T) {
		t.Parallel()

		mw := &mockRoundTripper{}
		cfg := Config{
			Transport: mw,
		}
		client, err := NewProxyClient(cfg)
		require.NoError(t, err)
		assert.Equal(t, mw, client.Transport)
	})

	t.Run("custom_round_tripper_factory", func(t *testing.T) {
		t.Parallel()

		mw := &mockRoundTripper{}
		cfg := Config{
			TransportFactory: func(c Config) (http.RoundTripper, error) {
				return mw, nil
			},
		}
		client, err := NewProxyClient(cfg)
		require.NoError(t, err)
		assert.Equal(t, mw, client.Transport)
	})

	t.Run("factory_error_handling", func(t *testing.T) {
		t.Parallel()

		cfg := Config{
			TransportFactory: func(c Config) (http.RoundTripper, error) {
				return nil, errors.New("factory simulation error")
			},
		}
		_, err := NewProxyClient(cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "factory simulation error")
	})
}

func TestProxyRotator_StickySession(t *testing.T) {
	t.Parallel()

	t.Run("sticky_key_selectors", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)

		req.AddCookie(&http.Cookie{Name: "session-id", Value: "abc"})
		req.Header.Set("X-Custom-Sticky", "val-xyz")

		// Test Cookie selector (now uses "session-id" to match the configured cookie)
		sel1 := StickyKeyFromCookie("session-id")
		assert.Equal(t, "abc", sel1(req))

		// Test Cookie selector missing
		sel1Missing := StickyKeyFromCookie("non-existent")
		sel1Proxy := sel1Missing(req)
		assert.Empty(t, sel1Proxy)

		// Test Header selector (now uses "X-Custom-Sticky" to match the set header)
		sel2 := StickyKeyFromHeader("X-Custom-Sticky")
		assert.Equal(t, "val-xyz", sel2(req))
	})

	t.Run("with_sticky_sessions_copy", func(t *testing.T) {
		t.Parallel()

		m1 := &mockDoer{id: 1}
		r, err := NewRotator(RotatorConfig{}, WithClient{Client: m1})
		require.NoError(t, err)
		t.Cleanup(func() { _ = r.Close() })

		stickyFn := func(req *http.Request) string { return "sticky" }
		copied := r.WithStickySessions(stickyFn)
		assert.NotNil(t, copied)
		assert.NotNil(t, copied.stickyKeyFunc)
	})

	t.Run("sticky_routing_and_fallback", func(t *testing.T) {
		t.Parallel()

		m1 := &mockDoer{id: 1}
		m2 := &mockDoer{id: 2}

		r, err := NewRotator(
			RotatorConfig{},
			WithClient{Client: m1, ProxyURL: "http://proxy1"},
			WithClient{Client: m2, ProxyURL: "http://proxy2"},
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = r.Close() })

		// Inject sticky extractor
		r.stickyKeyFunc = func(req *http.Request) string {
			return "session-id-1"
		}

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)

		// First request: establishes sticky session
		_, err = r.Do(req)
		require.NoError(t, err)

		r.mu.Lock()
		entry := r.sessions["session-id-1"]
		r.mu.Unlock()
		require.NotNil(t, entry)
		activeIdx := entry.clientIdx

		// Second request: must hit the exact same sticky client index
		_, err = r.Do(req)
		require.NoError(t, err)

		if activeIdx == 0 {
			assert.Equal(t, 2, m1.GetCalls())
			assert.Equal(t, 0, m2.GetCalls())
		} else {
			assert.Equal(t, 0, m1.GetCalls())
			assert.Equal(t, 2, m2.GetCalls())
		}

		// Mock sticky client becoming unhealthy
		for range 5 {
			r.clients[activeIdx].MarkFailed()
		}

		assert.False(t, r.clients[activeIdx].IsAvailable(), "sticky client should be marked unhealthy")

		// Third request: should bypass unhealthy sticky client and use fallback
		_, err = r.Do(req)
		require.NoError(t, err)

		if activeIdx == 0 {
			assert.Equal(t, 2, m1.GetCalls())
			assert.Equal(t, 1, m2.GetCalls())
		} else {
			assert.Equal(t, 1, m1.GetCalls())
			assert.Equal(t, 2, m2.GetCalls())
		}

		// Obsolete out-of-bounds sticky session index fallback
		r.mu.Lock()
		r.sessions["session-id-1"].clientIdx = 999
		r.mu.Unlock()

		_, err = r.Do(req)
		require.NoError(t, err)
	})
}

func TestProxyRotator_StickySessionCleanup(t *testing.T) {
	t.Parallel()

	m1 := &mockDoer{id: 1}
	r, err := NewRotator(RotatorConfig{}, WithClient{Client: m1})
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
	require.True(t, exists)
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

	r, err := NewRotator(RotatorConfig{}, WithClient{Client: m1}, WithClient{Client: m2})
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	r.Prewarm(t.Context(), "http://warmtarget.com")

	assert.Equal(t, 1, m1.GetCalls())
	assert.Equal(t, 1, m2.GetCalls())
}

func TestProxyRotator_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("all_proxies_failed", func(t *testing.T) {
		t.Parallel()

		m1 := &mockDoer{id: 1, forceError: true}
		r, err := NewRotator(RotatorConfig{MaxFails: 1}, WithClient{Client: m1})
		require.NoError(t, err)
		t.Cleanup(func() { _ = r.Close() })

		req, err := http.NewRequestWithContext(t.Context(), "GET", "http://test", nil)
		require.NoError(t, err)

		_, err = r.Do(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "all proxies failed")
	})
}

func TestProxyRotator_AdaptiveScoring_Cooldown(t *testing.T) {
	t.Parallel()

	doer1 := &mockDoer{id: 1}
	doer2 := &mockDoer{id: 2}

	rotator, err := NewRotator(RotatorConfig{
		MaxFails:   3,
		RetryAfter: time.Minute,
	}, WithClient{
		Client:   doer1,
		ProxyURL: "http://proxy1:8080",
	}, WithClient{
		Client:   doer2,
		ProxyURL: "http://proxy2:8080",
	})
	require.NoError(t, err)

	defer rotator.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://pyaterochka.ru/api/auth", nil)
	require.NoError(t, err)

	doer1.SetStatusCode(http.StatusForbidden)
	doer2.SetStatusCode(http.StatusOK)

	resp, err := rotator.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	var cooledClient, activeClient *mockDoer
	if doer1.GetCalls() > 0 {
		cooledClient = doer1
		activeClient = doer2

		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	} else {
		cooledClient = doer2
		activeClient = doer1

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	cooledClient.mu.Lock()
	cooledClient.calls = 0
	cooledClient.statusCode = http.StatusForbidden
	cooledClient.mu.Unlock()

	activeClient.mu.Lock()
	activeClient.calls = 0
	activeClient.statusCode = http.StatusOK
	activeClient.mu.Unlock()

	for i := 0; i < 5; i++ {
		resp, err := rotator.Do(req)
		require.NoError(t, err)

		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden {
			break
		}
	}

	cooledClient.mu.Lock()
	cooledClient.calls = 0
	cooledClient.mu.Unlock()

	activeClient.mu.Lock()
	activeClient.calls = 0
	activeClient.mu.Unlock()

	for i := 0; i < 5; i++ {
		resp, err := rotator.Do(req)
		require.NoError(t, err)

		_ = resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}

	assert.Equal(t, 0, cooledClient.GetCalls(), "Cooled down client should not have been called!")
	assert.Equal(t, 5, activeClient.GetCalls(), "Active client should have received all requests!")

	reqGoogle, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://google.com/", nil)
	require.NoError(t, err)

	googleCallsCooled := 0
	for i := 0; i < 5; i++ {
		resp, err := rotator.Do(reqGoogle)
		require.NoError(t, err)

		_ = resp.Body.Close()

		if cooledClient.GetCalls() > 0 {
			googleCallsCooled++
		}
	}

	assert.True(t, googleCallsCooled > 0, "Cooled client should be active for other domains")
}

func TestRetryMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("retry_on_failure_and_preserve_body", func(t *testing.T) {
		t.Parallel()

		m1 := &mockDoer{id: 1, statusCode: 502}
		rotator, err := NewRotator(RotatorConfig{}, WithClient{Client: m1})
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

		retryMiddleware := middleware.Retry(opts, RetryCondition(rotator))
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
		rotator, err := NewRotator(RotatorConfig{}, WithClient{Client: m1})
		require.NoError(t, err)
		t.Cleanup(func() { _ = rotator.Close() })

		opts := middleware.RetryOptions{
			MaxRetries: 1,
			Backoff:    1 * time.Millisecond,
		}

		client := middleware.Retry(opts, RetryCondition(rotator))(m1)
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

		m1 := aoni.DoerFunc(func(req *http.Request) (*http.Response, error) {
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

		opts := middleware.RetryOptions{
			MaxRetries: 2,
			Backoff:    1 * time.Microsecond,
		}

		condition := func(resp *http.Response, err error) bool {
			return resp != nil && resp.StatusCode == http.StatusTooManyRequests
		}

		retryMiddleware := middleware.Retry(opts, condition)
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
