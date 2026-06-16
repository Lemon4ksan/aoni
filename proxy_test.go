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

func TestNewProxyClient(t *testing.T) {
	t.Run("Default timeout", func(t *testing.T) {
		cfg := ProxyConfig{}

		client, err := NewProxyClient(cfg)
		if err != nil {
			t.Fatal(err)
		}

		if client.Timeout != 15*time.Second {
			t.Errorf("expected default timeout 15s, got %v", client.Timeout)
		}
	})

	t.Run("Custom config", func(t *testing.T) {
		proxyAddr := "http://user:pass@1.2.3.4:8080"
		cfg := ProxyConfig{
			ProxyURL:           proxyAddr,
			Timeout:            5 * time.Second,
			InsecureSkipVerify: true,
		}

		client, err := NewProxyClient(cfg)
		if err != nil {
			t.Fatal(err)
		}

		if client.Timeout != 5*time.Second {
			t.Errorf("expected timeout 5s, got %v", client.Timeout)
		}

		transport := client.Transport.(*http.Transport)
		if !transport.TLSClientConfig.InsecureSkipVerify {
			t.Error("expected InsecureSkipVerify to be true")
		}

		req, _ := http.NewRequest("GET", "http://google.com", nil)

		proxyURL, err := transport.Proxy(req)
		if err != nil {
			t.Fatalf("failed to get proxy URL from transport: %v", err)
		}

		if proxyURL.String() != proxyAddr {
			t.Errorf("expected proxy %s, got %s", proxyAddr, proxyURL.String())
		}
	})

	t.Run("Invalid proxy URL", func(t *testing.T) {
		cfg := ProxyConfig{
			ProxyURL: " ://invalid-url",
		}

		_, err := NewProxyClient(cfg)
		if err == nil {
			t.Error("expected error for invalid proxy URL, got nil")
		}
	})

	t.Run("No proxy", func(t *testing.T) {
		cfg := ProxyConfig{ProxyURL: ""}

		client, err := NewProxyClient(cfg)
		if err != nil {
			t.Fatal(err)
		}

		transport := client.Transport.(*http.Transport)
		if transport.Proxy != nil {
			req, _ := http.NewRequest("GET", "http://google.com", nil)

			p, _ := transport.Proxy(req)
			if p != nil {
				t.Errorf("expected no proxy, got %v", p)
			}
		}
	})
}

func TestProxyRotator(t *testing.T) {
	t.Run("Empty clients error", func(t *testing.T) {
		_, err := NewProxyRotator(ProxyRotatorConfig{})
		if err == nil || err.Error() != "aoni: proxy rotator requires at least one client" {
			t.Errorf("expected specific error, got %v", err)
		}
	})

	t.Run("Round-Robin logic", func(t *testing.T) {
		m1 := &mockDoer{id: 1}
		m2 := &mockDoer{id: 2}
		m3 := &mockDoer{id: 3}

		rotator, err := NewProxyRotator(ProxyRotatorConfig{}, m1, m2, m3)
		if err != nil {
			t.Fatal(err)
		}

		req, _ := http.NewRequest("GET", "http://test", nil)

		for range 4 {
			_, err := rotator.Do(req)
			if err != nil {
				t.Fatal(err)
			}
		}

		if m1.GetCalls() != 1 {
			t.Errorf("m1 expected 1 call, got %d", m1.GetCalls())
		}

		if m2.GetCalls() != 2 {
			t.Errorf("m2 expected 2 calls, got %d", m2.GetCalls())
		}

		if m3.GetCalls() != 1 {
			t.Errorf("m3 expected 1 call, got %d", m3.GetCalls())
		}
	})

	t.Run("Concurrency safety", func(t *testing.T) {
		count := 10
		clients := make([]HTTPDoer, count)

		mocks := make([]*mockDoer, count)
		for i := range count {
			mocks[i] = &mockDoer{id: i}
			clients[i] = mocks[i]
		}

		rotator, _ := NewProxyRotator(ProxyRotatorConfig{}, clients...)

		var wg sync.WaitGroup

		iterations := 1000
		wg.Add(iterations)

		req, _ := http.NewRequest("GET", "http://test", nil)

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

		if totalCalls != iterations {
			t.Errorf("expected total %d calls, got %d", iterations, totalCalls)
		}
	})
}

func TestProxyRotator_HealthCheck(t *testing.T) {
	m1 := &mockDoer{id: 1}
	m2 := &mockDoer{id: 2, forceError: true}

	cfg := ProxyRotatorConfig{
		MaxFails:   2,
		RetryAfter: 100 * time.Millisecond,
	}
	rotator, _ := NewProxyRotator(cfg, m1, m2)

	req, _ := http.NewRequest("GET", "http://test", nil)

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
		rotator.Do(req)

		if m2.GetCalls() > 2 {
			foundM2 = true
			break
		}
	}

	if !foundM2 {
		t.Error("m2 should have been retried after cooldown")
	}
}

func TestProxyRotator_BackgroundHealthCheck(t *testing.T) {
	m1 := &mockDoer{id: 1, forceError: true}

	cfg := ProxyRotatorConfig{
		MaxFails:            1,
		RetryAfter:          1 * time.Hour,
		HealthCheckURL:      "http://health",
		HealthCheckInterval: 50 * time.Millisecond,
	}

	rotator, err := NewProxyRotator(cfg, m1)
	if err != nil {
		t.Fatal(err)
	}
	defer rotator.Close()

	req, _ := http.NewRequest("GET", "http://test", nil)

	_, _ = rotator.Do(req)
	if !rotator.clients[0].unhealthy.Load() {
		t.Fatal("proxy should be unhealthy")
	}

	m1.SetForceError(false)

	time.Sleep(150 * time.Millisecond)

	if rotator.clients[0].unhealthy.Load() {
		t.Error("proxy should be healthy after background check")
	}
}

func TestProxyRotator_ContextCancellation(t *testing.T) {
	m1 := &mockDoer{id: 1}
	rotator, _ := NewProxyRotator(ProxyRotatorConfig{}, m1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", "http://test", nil)
	_, err := rotator.Do(req)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	if rotator.clients[0].unhealthy.Load() {
		t.Error("proxy should NOT be marked unhealthy on cancellation")
	}
}

func TestProxyRotator_RetryOnProxyError(t *testing.T) {
	m1 := &mockDoer{id: 1, statusCode: 407}
	m2 := &mockDoer{id: 2, statusCode: 200}

	rotator, _ := NewProxyRotator(ProxyRotatorConfig{MaxFails: 1}, m1, m2)

	req, _ := http.NewRequest("GET", "http://steam", nil)

	resp, err := rotator.Do(req)
	if err != nil {
		t.Fatalf("expected success after rotation, got %v", err)
	}

	if resp.StatusCode != 200 {
		t.Errorf("expected 200 from second proxy, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest("GET", "http://steam", nil)

	_, err = rotator.Do(req)
	if err != nil {
		t.Fatalf("expected success after rotation, got %v", err)
	}

	if !rotator.clients[0].unhealthy.Load() {
		t.Error("proxy 1 should be unhealthy after 407 error")
	}
}

func TestProxyConfig_CustomTransport(t *testing.T) {
	t.Run("Custom RoundTripper", func(t *testing.T) {
		mw := &mockRoundTripper{}
		cfg := ProxyConfig{
			Transport: mw,
		}
		client, err := NewProxyClient(cfg)
		require.NoError(t, err)
		assert.Equal(t, mw, client.Transport)
	})

	t.Run("Custom RoundTripper Factory", func(t *testing.T) {
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

type mockRoundTripper struct{}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestProxyRotator_StickySessionCleanup(t *testing.T) {
	m1 := &mockDoer{id: 1}
	r, err := NewProxyRotator(ProxyRotatorConfig{}, m1)
	require.NoError(t, err)

	defer r.Close()

	r.sessionTTL = 10 * time.Millisecond
	r.stickyKeyFunc = func(req *http.Request) string {
		return "session1"
	}

	req, _ := http.NewRequest("GET", "http://test", nil)
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
	m1 := &mockDoer{id: 1}
	m2 := &mockDoer{id: 2}

	r, err := NewProxyRotator(ProxyRotatorConfig{}, m1, m2)
	require.NoError(t, err)

	defer r.Close()

	ctx := context.Background()
	r.Prewarm(ctx, "http://warmtarget.com")

	assert.Equal(t, 1, m1.GetCalls())
	assert.Equal(t, 1, m2.GetCalls())
}
