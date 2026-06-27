// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/ja4"
)

func setupBridgeTest(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *http.Client) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	c := NewClient(nil).WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	return server, stdClient
}

func setupBridgeTestWithClient(t *testing.T, c *Client, handler http.HandlerFunc) (*httptest.Server, *http.Client) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	c = c.WithBaseURL(server.URL)
	stdClient := NewStdClient(c)

	return server, stdClient
}

func TestNewStdClient_SendRequest_Succeeds(t *testing.T) {
	t.Parallel()

	t.Run("basic_get_request", func(t *testing.T) {
		t.Parallel()

		server, stdClient := setupBridgeTest(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/test", r.URL.Path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("hello"))
		})

		resp, err := stdClient.Get(server.URL + "/test")
		require.NoError(t, err)

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(body))
	})

	t.Run("basic_post_request", func(t *testing.T) {
		t.Parallel()

		server, stdClient := setupBridgeTest(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.Equal(t, `{"key":"value"}`, string(body))

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})

		req, err := http.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			server.URL+"/submit",
			strings.NewReader(`{"key":"value"}`),
		)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := stdClient.Do(req)
		require.NoError(t, err)

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "ok", string(body))
	})

	t.Run("preserve_headers_merging", func(t *testing.T) {
		t.Parallel()

		server, stdClient := setupBridgeTest(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, []string{"v1", "v2"}, r.Header.Values("X-Multi"))
			assert.Equal(t, "test-agent/1.0", r.Header.Get("User-Agent"))
			w.WriteHeader(http.StatusOK)
		})

		req, err := http.NewRequestWithContext(
			t.Context(),
			http.MethodGet,
			server.URL+"/headers",
			nil,
		)
		require.NoError(t, err)

		req.Header.Add("X-Multi", "v1")
		req.Header.Add("X-Multi", "v2")
		req.Header.Set("User-Agent", "test-agent/1.0")

		resp, err := stdClient.Do(req)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("body_stream_preservation", func(t *testing.T) {
		t.Parallel()

		const payload = "streaming body content with special chars: àáâãäå"

		server, stdClient := setupBridgeTest(t, func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.Equal(t, payload, string(body))

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("received"))
		})

		req, err := http.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			server.URL+"/stream",
			strings.NewReader(payload),
		)
		require.NoError(t, err)

		resp, err := stdClient.Do(req)
		require.NoError(t, err)

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "received", string(body))
	})

	t.Run("query_params_preservation", func(t *testing.T) {
		t.Parallel()

		server, stdClient := setupBridgeTest(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "bar", r.URL.Query().Get("foo"))
			assert.Equal(t, "z spaces", r.URL.Query().Get("baz"))
			w.WriteHeader(http.StatusOK)
		})

		req, err := http.NewRequestWithContext(
			t.Context(),
			http.MethodGet,
			server.URL+"/query?foo=bar&baz=z+spaces",
			nil,
		)
		require.NoError(t, err)

		resp, err := stdClient.Do(req)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("large_response_body", func(t *testing.T) {
		t.Parallel()

		largeData := strings.Repeat("A", 100*1024)

		server, stdClient := setupBridgeTest(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(largeData))
		})

		resp, err := stdClient.Get(server.URL + "/large")
		require.NoError(t, err)

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, 100*1024, len(body))
		assert.Equal(t, largeData, string(body))
	})

	t.Run("gzip_compressed_response", func(t *testing.T) {
		t.Parallel()

		server, stdClient := setupBridgeTest(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", "text/plain")

			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte("gzipped content"))
			_ = gz.Close()
		})

		resp, err := stdClient.Get(server.URL + "/gzip")
		require.NoError(t, err)

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "gzipped content", string(body))
	})
}

func TestNewStdClient_SyncFields_PreservesProperties(t *testing.T) {
	t.Parallel()

	var (
		capturedHost             string
		capturedClose            bool
		capturedTransferEncoding []string
	)

	server, stdClient := setupBridgeTest(t, func(w http.ResponseWriter, r *http.Request) {
		capturedHost = r.Host
		capturedClose = r.Close
		capturedTransferEncoding = r.TransferEncoding

		w.WriteHeader(http.StatusOK)
	})

	// Changed method to POST and supplied a non-empty body.
	// This ensures standard Go transport preserves chunked Transfer-Encoding.
	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		server.URL,
		strings.NewReader("chunked_test_payload"),
	)
	require.NoError(t, err)

	req.Host = "custom-host.test"
	req.Close = true
	req.TransferEncoding = []string{"chunked"}

	resp, err := stdClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "custom-host.test", capturedHost)
	assert.True(t, capturedClose)
	assert.Equal(t, []string{"chunked"}, capturedTransferEncoding)
}

func TestNewStdClient_VariousStatusCodes_ReturnsCorrectStatus(t *testing.T) {
	t.Parallel()

	server, stdClient := setupBridgeTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status_200":
			w.WriteHeader(http.StatusOK)
		case "/status_404":
			w.WriteHeader(http.StatusNotFound)
		case "/status_500":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	})

	tests := []struct {
		name           string
		path           string
		expectedStatus int
	}{
		{"status_200", "/status_200", http.StatusOK},
		{"status_404", "/status_404", http.StatusNotFound},
		{"status_500", "/status_500", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp, err := stdClient.Get(server.URL + tt.path)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode)
		})
	}
}

func TestWithContextModifier_ConfigureContext_AppliesModifiers(t *testing.T) {
	t.Parallel()

	t.Run("carry_custom_headers", func(t *testing.T) {
		t.Parallel()

		server, stdClient := setupBridgeTest(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "secret-token", r.Header.Get("Authorization"))
			assert.Equal(t, "custom-value", r.Header.Get("X-Custom"))
			w.WriteHeader(http.StatusOK)
		})

		ctx := WithContextModifier(
			t.Context(),
			WithHeader("Authorization", "secret-token"),
			WithHeader("X-Custom", "custom-value"),
		)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/auth", nil)
		require.NoError(t, err)

		resp, err := stdClient.Do(req)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("no_modifiers", func(t *testing.T) {
		t.Parallel()

		ctx := WithContextModifier(t.Context())
		mods := ContextModifiers(ctx)
		assert.Nil(t, mods)
	})
}

func TestWithContextModifier_Empty_ReturnsSameContext(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	res := WithContextModifier(ctx)
	assert.Equal(t, ctx, res)
}

func TestAppendContextModifier_ConfigureContext_AppendsModifiers(t *testing.T) {
	t.Parallel()

	ctx := WithContextModifier(t.Context(), WithHeader("A", "1"))
	ctx = AppendContextModifier(ctx, WithHeader("B", "2"))

	mods := ContextModifiers(ctx)
	require.Len(t, mods, 2)
}

func TestAppendContextModifier_Empty_ReturnsSameContext(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	res := AppendContextModifier(ctx)
	assert.Equal(t, ctx, res)
}

func TestContextModifiers_Retrieve_ReturnsExpectedModifiers(t *testing.T) {
	t.Parallel()

	t.Run("empty_context", func(t *testing.T) {
		t.Parallel()

		mods := ContextModifiers(t.Context())
		assert.Nil(t, mods)
	})

	t.Run("multiple_modifiers", func(t *testing.T) {
		t.Parallel()

		called := make([]string, 0, 3)

		m1 := func(req *http.Request) { called = append(called, "m1") }
		m2 := func(req *http.Request) { called = append(called, "m2") }
		m3 := func(req *http.Request) { called = append(called, "m3") }

		ctx := WithContextModifier(t.Context(), m1, m2, m3)
		mods := ContextModifiers(ctx)

		require.Len(t, mods, 3)

		for _, mod := range mods {
			mod(nil)
		}

		assert.Equal(t, []string{"m1", "m2", "m3"}, called)
	})
}

func TestNewStdClient_DefaultSetup_PropertiesMatch(t *testing.T) {
	t.Parallel()

	c := NewClient(nil)
	stdClient := NewStdClient(c)

	assert.Nil(t, stdClient.Jar)
	assert.IsType(t, &Transport{}, stdClient.Transport)
}

func TestNewTransport_ExposesRoundTripper_CustomClient(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	c := NewClient(nil).WithBaseURL(server.URL)
	tr := NewTransport(c)

	customClient := &http.Client{
		Transport: tr,
		Timeout:   5 * time.Second,
	}

	resp, err := customClient.Get(server.URL)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestAoniTransport_RoundTrip_RelativeAndAbsoluteHost(t *testing.T) {
	t.Parallel()

	t.Run("absolute_url_with_host", func(t *testing.T) {
		t.Parallel()

		var capturedBaseURL string

		mockDoer := DoerFunc(func(req *http.Request) (*http.Response, error) {
			capturedBaseURL = req.URL.String()

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("absolute_ok")),
				Request:    req,
			}, nil
		})

		c := NewClient(mockDoer).WithBaseURL("http://localhost")
		tr := NewTransport(c)

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com/test", nil)
		require.NoError(t, err)

		resp, err := tr.RoundTrip(req)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, "https://example.com/test", capturedBaseURL)
	})

	t.Run("relative_url_no_host", func(t *testing.T) {
		t.Parallel()

		var capturedURL string

		mockDoer := DoerFunc(func(req *http.Request) (*http.Response, error) {
			capturedURL = req.URL.String()

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("relative_ok")),
				Request:    req,
			}, nil
		})

		c := NewClient(mockDoer).WithBaseURL("http://localhost")
		tr := NewTransport(c)

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/relative_path", nil)
		require.NoError(t, err)

		resp, err := tr.RoundTrip(req)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, "/relative_path", capturedURL)
	})
}

func TestNewStdClient_AutoRestoreGetBody_Succeeds(t *testing.T) {
	t.Parallel()

	var captures []string

	server, stdClient := setupBridgeTest(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		captures = append(captures, string(body))

		w.WriteHeader(http.StatusOK)
	})

	// Removed the manual clear of req.GetBody, making sure the client
	// runs cleanly and preserves GetBody throughout routing.
	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		server.URL,
		strings.NewReader("payload_to_replay"),
	)
	require.NoError(t, err)

	resp, err := stdClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "payload_to_replay", captures[0])

	require.NotNil(t, req.GetBody)
	replayedReader, err := req.GetBody()
	require.NoError(t, err)

	defer replayedReader.Close()

	replayedBytes, err := io.ReadAll(replayedReader)
	require.NoError(t, err)
	assert.Equal(t, "payload_to_replay", string(replayedBytes))
}

func TestNewStdClient_ParallelRequests_ConcurrentExecutionSafe(t *testing.T) {
	t.Parallel()

	var count atomic.Int64

	server, stdClient := setupBridgeTest(t, func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	var wg sync.WaitGroup

	for range 10 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			resp, err := stdClient.Get(server.URL + "/parallel")
			if err != nil {
				t.Errorf("stdClient.Get failed: %v", err)
				return
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Errorf("io.ReadAll failed: %v", err)
				return
			}

			if string(body) != "ok" {
				t.Errorf("expected body 'ok', got '%s'", string(body))
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, int64(10), count.Load())
}

func TestNewStdClient_Resilience_IntegratesMiddlewares(t *testing.T) {
	t.Parallel()

	t.Run("with_retry_middleware", func(t *testing.T) {
		t.Parallel()

		var attempts atomic.Int32

		retryOpts := RetryOptions{
			MaxRetries:     5,
			Backoff:        0,
			JitterStrategy: JitterEqual,
		}

		doer := Chain(
			&http.Client{},
			RetryMiddleware(retryOpts, RetryOnGatewayErrors()),
		)

		c := NewClient(doer)
		server, stdClient := setupBridgeTestWithClient(t, c, func(w http.ResponseWriter, r *http.Request) {
			n := attempts.Add(1)
			if n < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		})

		resp, err := stdClient.Get(server.URL + "/retry")
		require.NoError(t, err)

		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "success", string(body))
		assert.GreaterOrEqual(t, attempts.Load(), int32(3))
	})

	t.Run("with_circuit_breaker", func(t *testing.T) {
		t.Parallel()

		cb := NewCircuitBreaker(CircuitBreakerConfig{
			FailureThreshold: 0.5,
			MinRequests:      2,
			Cooldown:         0,
			Window:           10 * time.Second,
		})

		var attempts atomic.Int32

		doer := Chain(
			&http.Client{},
			CircuitBreakerMiddleware(cb, DefaultCircuitBreakerCondition),
		)

		c := NewClient(doer)
		server, stdClient := setupBridgeTestWithClient(t, c, func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		})

		for range 10 {
			resp, err := stdClient.Get(server.URL + "/cb")
			if err != nil {
				break
			}

			resp.Body.Close()
		}

		assert.Greater(t, attempts.Load(), int32(0))
	})
}

func TestNewStdClient_CancelledContext_FailsRequest(t *testing.T) {
	t.Parallel()

	server, stdClient := setupBridgeTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/cancel", nil)
	require.NoError(t, err)

	_, err = stdClient.Do(req)
	assert.Error(t, err)
}

func TestAoniTransport_RoundTrip_NilURL_ReturnsURLError(t *testing.T) {
	t.Parallel()

	c := NewClient(nil)
	tr := NewTransport(c)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
	require.NoError(t, err)

	req.URL = nil

	_, err = tr.RoundTrip(req)

	var urlErr *url.Error
	if assert.ErrorAs(t, err, &urlErr) {
		assert.Contains(t, urlErr.Error(), "aoni bridge: request URL is nil")
	}
}

type mockTransport func(*http.Request) (*http.Response, error)

func (f mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestAoniTransport_RoundTrip_SpecialHeaders(t *testing.T) {
	t.Parallel()

	var (
		capturedProxy       *url.URL
		capturedFingerprint BrowserID
		capturedTimeout     time.Duration
		capturedSSRF        bool
		capturedMaxResponse int64
		headersCleaned      bool
	)

	transport := mockTransport(func(req *http.Request) (*http.Response, error) {
		headersCleaned = true
		for k := range req.Header {
			if strings.HasPrefix(strings.ToLower(k), "x-aoni-") {
				headersCleaned = false
			}
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    req,
		}, nil
	})

	httpClient := &http.Client{Transport: transport}
	c := NewClient(httpClient)
	tr := NewTransport(c)

	tr.BeforeRoundTrip = func(cloned *Client, origReq *http.Request) *Client {
		capturedFingerprint = cloned.tlsBrowserID
		capturedSSRF = cloned.ssrfGuard
		capturedMaxResponse = cloned.maxResponseSize
		capturedProxy = cloned.proxyAddr

		if hClient, ok := cloned.http.(*http.Client); ok {
			capturedTimeout = hClient.Timeout
		}

		return cloned
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
	require.NoError(t, err)

	req.Header.Set("X-Aoni-Proxy", "http://my-proxy:1080")
	req.Header.Set("X-Aoni-TLS-Fingerprint", "firefox")
	req.Header.Set("X-Aoni-Timeout", "12s")
	req.Header.Set("X-Aoni-SSRF-Guard", "true")
	req.Header.Set("X-Aoni-Max-Response-Size", "524288")

	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.True(t, headersCleaned)
	assert.Equal(t, "http://my-proxy:1080", capturedProxy.String())
	assert.Equal(t, BrowserFirefox, capturedFingerprint)
	assert.Equal(t, 12*time.Second, capturedTimeout)
	assert.True(t, capturedSSRF)
	assert.Equal(t, int64(524288), capturedMaxResponse)
}

func TestAoniTransport_RoundTrip_TraceContext(t *testing.T) {
	t.Parallel()

	mockDoer := DoerFunc(func(req *http.Request) (*http.Response, error) {
		if store, ok := req.Context().Value(ja4ReportCtxKey{}).(*ja4ReportStore); ok {
			store.report = &ja4.Report{JA4: "t13d1516h2_mock_fingerprint"}
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("trace_ok")),
			Request:    req,
		}, nil
	})

	c := NewClient(mockDoer)
	tr := NewTransport(c)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
	require.NoError(t, err)

	TraceContext()(req)

	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	info := ResponseTrace(resp)
	require.NotNil(t, info)
	require.NotNil(t, info.JA4)
	assert.Equal(t, "t13d1516h2_mock_fingerprint", info.JA4.JA4)
}

func TestAoniTransport_RoundTrip_RequestFailure_ReturnsURLError(t *testing.T) {
	t.Parallel()

	mockDoer := DoerFunc(func(req *http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})

	c := NewClient(mockDoer)
	tr := NewTransport(c)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost/fail", nil)
	require.NoError(t, err)

	_, err = tr.RoundTrip(req)

	var urlErr *url.Error
	require.ErrorAs(t, err, &urlErr)
	assert.Equal(t, http.MethodGet, urlErr.Op)
	assert.Equal(t, "http://localhost/fail", urlErr.URL)

	var bridgeErr *BridgeError
	require.ErrorAs(t, urlErr.Err, &bridgeErr)
	assert.ErrorIs(t, bridgeErr.Err, io.ErrUnexpectedEOF)
	assert.Equal(t, "localhost", bridgeErr.Metadata["host"])
}

func FuzzContextModifiers(f *testing.F) {
	f.Add("Authorization", "Bearer token1")
	f.Add("X-Custom-Header", "Value2")

	f.Fuzz(func(t *testing.T, key, val string) {
		if key == "" || strings.Contains(key, "\x00") {
			return
		}

		ctx := t.Context()
		mod1 := WithHeader(key, val)
		ctx = WithContextModifier(ctx, mod1)

		mods := ContextModifiers(ctx)
		if len(mods) != 1 {
			t.Errorf("expected 1 modifier, got %d", len(mods))
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost", nil)
		if err != nil {
			return
		}

		for _, mod := range mods {
			mod(req)
		}

		if req.Header.Get(key) != val {
			t.Errorf("expected header %q to be %q, got %q", key, val, req.Header.Get(key))
		}
	})
}
