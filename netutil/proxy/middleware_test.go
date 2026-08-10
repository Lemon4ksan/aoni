// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

type customResponseAdapter struct {
	statusCode int
}

func (c *customResponseAdapter) StatusCode() int                   { return c.statusCode }
func (c *customResponseAdapter) Status() string                    { return http.StatusText(c.statusCode) }
func (c *customResponseAdapter) StatusBytes() []byte               { return []byte(c.Status()) }
func (c *customResponseAdapter) Header(_ string) string            { return "" }
func (c *customResponseAdapter) HeaderBytes(_ []byte) []byte       { return nil }
func (c *customResponseAdapter) Headers() map[string][]string      { return nil }
func (c *customResponseAdapter) BodyBytes() []byte                 { return nil }
func (c *customResponseAdapter) BodyStream() io.ReadCloser         { return nil }
func (c *customResponseAdapter) HTTPResponse() *http.Response      { return nil } // Simulates fast.Response adapter
func (c *customResponseAdapter) EngineResponse() any               { return nil }
func (c *customResponseAdapter) Uncompressed() bool                { return false }
func (c *customResponseAdapter) SetUncompressed(_ bool)            {}
func (c *customResponseAdapter) Close() error                      { return nil }
func (c *customResponseAdapter) Trailers() map[string][]string     { return nil }
func (c *customResponseAdapter) SetTrailers(_ map[string][]string) {}
func (c *customResponseAdapter) UnsafeBodyBytes() []byte           { return nil }

func TestProxy_RetryCondition(t *testing.T) {
	t.Parallel()

	m1 := &mockDoer{id: 1}
	rotator, err := proxy.NewRotator(proxy.RotatorConfig{}, proxy.WithClient{Client: m1})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rotator.Close() })

	cond := proxy.RetryCondition(rotator)

	t.Run("retry_on_network_error", func(t *testing.T) {
		t.Parallel()

		netErr := errors.New("dial tcp: connection refused")
		assert.True(t, cond(nil, netErr))
	})

	t.Run("do_not_retry_on_context_canceled", func(t *testing.T) {
		t.Parallel()
		assert.False(t, cond(nil, context.Canceled))
	})

	t.Run("adapter_response_fault_status_codes", func(t *testing.T) {
		t.Parallel()

		faultCodes := []int{
			http.StatusProxyAuthRequired,
			http.StatusTooManyRequests,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
		}

		for _, code := range faultCodes {
			resp := &customResponseAdapter{statusCode: code}
			assert.True(t, cond(resp, nil), "status code %d should trigger proxy retry", code)
		}

		okResp := &customResponseAdapter{statusCode: http.StatusOK}
		assert.False(t, cond(okResp, nil))
	})
}
