// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/foundation/borrow"
	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/resiliency/loadbalancer"
)

func TestLatencyProfile_Fast_Vs_StdHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping latency profiling in short mode")
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"ok","engine":"fast"}`)
	}))
	t.Cleanup(ts.Close)

	const (
		concurrency   = 100
		reqsPerWorker = 300
		totalReqs     = concurrency * reqsPerWorker
	)

	// 1. Profile aoni/fast
	fastClient := fast.NewClient(option.WithConnectionPool(aoni.ConnectionPoolConfig{
		MaxConnsPerHost: concurrency,
	}))
	fastLatencies := make([]time.Duration, totalReqs)

	var (
		fastIdx   int32
		wgFast    sync.WaitGroup
		fastStart = time.Now()
	)

	for range concurrency {
		wgFast.Add(1)

		go func() {
			defer wgFast.Done()

			ctx := context.Background()
			for range reqsPerWorker {
				t0 := time.Now()
				resp, err := fastClient.Request(ctx, http.MethodGet, ts.URL)
				elapsed := time.Since(t0)

				require.NoError(t, err)
				resp.Close()

				idx := atomic.AddInt32(&fastIdx, 1) - 1
				fastLatencies[idx] = elapsed
			}
		}()
	}

	wgFast.Wait()

	fastDuration := time.Since(fastStart)

	// 2. Profile std net/http
	stdClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: concurrency,
		},
	}
	stdLatencies := make([]time.Duration, totalReqs)

	var (
		stdIdx   int32
		wgStd    sync.WaitGroup
		stdStart = time.Now()
	)

	for range concurrency {
		wgStd.Add(1)

		go func() {
			defer wgStd.Done()

			for range reqsPerWorker {
				t0 := time.Now()
				req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL, nil)
				require.NoError(t, err)

				resp, err := stdClient.Do(req)
				require.NoError(t, err)

				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				elapsed := time.Since(t0)

				idx := atomic.AddInt32(&stdIdx, 1) - 1
				stdLatencies[idx] = elapsed
			}
		}()
	}

	wgStd.Wait()

	stdDuration := time.Since(stdStart)

	// 3. Compute metrics
	slices.Sort(fastLatencies)
	slices.Sort(stdLatencies)

	fastRPS := float64(totalReqs) / fastDuration.Seconds()
	stdRPS := float64(totalReqs) / stdDuration.Seconds()

	fastP50 := fastLatencies[int(float64(totalReqs)*0.50)]
	fastP90 := fastLatencies[int(float64(totalReqs)*0.90)]
	fastP99 := fastLatencies[int(float64(totalReqs)*0.99)]

	stdP50 := stdLatencies[int(float64(totalReqs)*0.50)]
	stdP90 := stdLatencies[int(float64(totalReqs)*0.90)]
	stdP99 := stdLatencies[int(float64(totalReqs)*0.99)]

	t.Logf("\n=== LATENCY & THROUGHPUT PROFILING (%d total requests, %d concurrency) ===", totalReqs, concurrency)
	t.Logf("[aoni/fast]   RPS: %8.0f req/s | p50: %8s | p90: %8s | p99: %8s", fastRPS, fastP50, fastP90, fastP99)
	t.Logf("[net/http]    RPS: %8.0f req/s | p50: %8s | p90: %8s | p99: %8s", stdRPS, stdP50, stdP90, stdP99)
	t.Logf(
		"Improvement:  fast is %0.1fx faster in RPS, p99 latency reduced by %0.1f%%",
		fastRPS/stdRPS,
		float64(stdP99-fastP99)/float64(stdP99)*100,
	)
}

func TestFastRequestAdapter(t *testing.T) {
	t.Parallel()

	req := fast.NewRequest(nil)
	t.Cleanup(req.Release)

	req.SetMethod(http.MethodPost)
	assert.Equal(t, http.MethodPost, req.Method())

	req.SetURL("https://example.com/api/test?foo=bar")
	assert.Equal(t, "https://example.com/api/test?foo=bar", req.URL())
	assert.Equal(t, "/api/test", req.Path())
	assert.Equal(t, "foo=bar", req.RawQuery())

	req.AddQueryParam("baz", "123")
	assert.Equal(t, "foo=bar&baz=123", req.RawQuery())

	req.SetHeader("X-Custom", "val")
	assert.Equal(t, "val", req.Header("X-Custom"))

	payload := []byte("hello world payload")
	req.SetBodyBytes(payload)
	assert.Equal(t, string(payload), string(req.BodyBytes()))
}

func TestFastResponseAdapter(t *testing.T) {
	t.Parallel()

	resp := fast.NewResponse(nil)
	t.Cleanup(func() { _ = resp.Close() })

	fastResp := resp.FastHTTPResponse()
	fastResp.SetStatusCode(http.StatusCreated)
	fastResp.Header.Set("Content-Type", "application/json")
	fastResp.SetBodyString(`{"status":"created"}`)

	assert.Equal(t, http.StatusCreated, resp.StatusCode())
	assert.Equal(t, "application/json", resp.Header("Content-Type"))
	assert.Equal(t, `{"status":"created"}`, string(resp.BodyBytes()))
}

func TestFastRequest_Contract(t *testing.T) {
	t.Parallel()

	fastReq := h1engine.AcquireRequest()
	t.Cleanup(func() { h1engine.ReleaseRequest(fastReq) })

	req := fast.NewRequest(fastReq)
	require.NotNil(t, req.FastHTTPRequest())
	assert.Nil(t, req.HTTPRequest())
	assert.Same(t, fastReq, req.EngineRequest())

	req.SetURL("http://example.com/api/v1?k=v")
	assert.Equal(t, http.MethodGet, req.Method())

	req.SetMethod(http.MethodPost)
	assert.Equal(t, http.MethodPost, req.Method())

	req.SetMethodBytes([]byte("PATCH"))
	assert.Equal(t, "PATCH", req.Method())

	assert.Equal(t, "/api/v1", req.Path())
	req.SetPath("/api/v2")
	assert.Equal(t, "/api/v2", req.Path())

	req.AddQueryParam("p1", "v1")
	req.AddQueryParamBytes([]byte("p2"), []byte("v2"))
	assert.Contains(t, req.RawQuery(), "p1=v1")
	assert.Contains(t, req.RawQuery(), "p2=v2")

	req.SetQueryParam("p1", "updated")
	assert.Contains(t, req.RawQuery(), "p1=updated")

	req.SetHeader("X-Header-1", "val1")
	req.SetHeaderBytes([]byte("X-Header-2"), []byte("val2"))

	assert.Equal(t, "val1", req.Header("X-Header-1"))
	assert.Equal(t, "val2", string(req.HeaderBytes([]byte("X-Header-2"))))

	req.DelHeaderBytes([]byte("X-Header-1"))
	assert.Empty(t, req.Header("X-Header-1"))

	bodyPayload := []byte(`{"key": "fast_value"}`)
	req.SetBodyBytes(bodyPayload)
	assert.Equal(t, bodyPayload, req.BodyBytes())

	req.ResetHeaders()
	assert.Empty(t, req.Header("X-Header-2"))
}

func TestFastRequest_UnifiedModifiers(t *testing.T) {
	t.Parallel()

	modifiers := []aoni.RequestModifier{
		mod.WithHeader("X-App-ID", "aoni-v1"),
		mod.WithHeaderBytes([]byte("X-Engine"), []byte("fast")),
		mod.WithBearer("test-secret-token"),
		mod.WithJSONBody(map[string]string{"foo": "bar"}),
		mod.WithQuery(map[string]string{"page": "1"}),
	}

	fastReq := h1engine.AcquireRequest()
	t.Cleanup(func() { h1engine.ReleaseRequest(fastReq) })
	fastReq.SetRequestURI("http://localhost/test")

	fReq := fast.NewRequest(fastReq)
	for _, m := range modifiers {
		m.Apply(fReq)
	}

	assert.Equal(t, "aoni-v1", fReq.Header("X-App-ID"))
	assert.Equal(t, "fast", string(fReq.HeaderBytes([]byte("X-Engine"))))
	assert.Equal(t, "Bearer test-secret-token", fReq.Header("Authorization"))
	assert.Equal(t, "application/json", fReq.Header("Content-Type"))
	assert.JSONEq(t, `{"foo": "bar"}`, string(fReq.BodyBytes()))
	assert.Equal(t, "page=1", fReq.RawQuery())
}

func TestFastResponse_Contract(t *testing.T) {
	t.Parallel()

	fastRespStruct := h1engine.AcquireResponse()
	t.Cleanup(func() { h1engine.ReleaseResponse(fastRespStruct) })

	fastRespStruct.SetStatusCode(http.StatusAccepted)
	fastRespStruct.Header.Set("X-Fast-Resp", "fast-val")
	fastRespStruct.SetBodyString("fast-response-body")

	fastResp := fast.NewResponse(fastRespStruct)
	assert.Equal(t, http.StatusAccepted, fastResp.StatusCode())
	assert.Equal(t, "Accepted", fastResp.Status())
	assert.Equal(t, "fast-val", fastResp.Header("X-Fast-Resp"))
	assert.Equal(t, []byte("fast-response-body"), fastResp.BodyBytes())
	assert.Same(t, fastRespStruct, fastResp.EngineResponse())

	bodyStream, err := io.ReadAll(fastResp.BodyStream())
	require.NoError(t, err)
	assert.Equal(t, []byte("fast-response-body"), bodyStream)
	require.NoError(t, fastResp.Close())
}

func TestPooledResponseSafety(t *testing.T) {
	t.Parallel()

	fastReq := fast.NewRequest(nil).FastHTTPRequest()
	fastResp := fast.NewResponse(nil).FastHTTPResponse()

	pooled := fast.NewPooledResponse(fastReq, fastResp)

	require.NoError(t, pooled.Close())
	require.NoError(t, pooled.Close()) // Second Close must be safe & idempotent
}

func TestClientHTTP1Execution(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Aoni-Test") != "active" {
			http.Error(w, "missing test header", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)

		_, _ = fmt.Fprint(w, "fast engine ok")
	}))
	t.Cleanup(ts.Close)

	client := fast.NewClient(option.WithBaseURL(ts.URL))

	resp, err := client.Request(t.Context(), http.MethodGet, "/",
		mod.WithHeader("X-Aoni-Test", "active"),
	)
	require.NoError(t, err)

	defer resp.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, "fast engine ok", string(resp.BodyBytes()))
}

func TestFastBridge_StdClient(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Bridge-Engine", "h1engine")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"bridged":true}`))
	}))
	defer ts.Close()

	fastClient := fast.NewClient(option.WithTimeout(5 * time.Second))
	stdClient := fast.NewStdClient(fastClient)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+"/test", nil)
	require.NoError(t, err)

	resp, err := stdClient.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "h1engine", resp.Header.Get("X-Bridge-Engine"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, `{"bridged":true}`, string(body))
}

func TestFastClient_WithOptions(t *testing.T) {
	t.Parallel()

	client := fast.NewClient(
		option.WithBaseURL("http://example.com/v1"),
		option.WithTimeout(10*time.Second),
		option.WithUserAgent("FastClient/1.0"),
		option.WithHeaders(map[string]string{"X-Custom-App": "FastApp"}),
	)

	cfg := client.Config()
	assert.Equal(t, "http://example.com/v1/", cfg.Defaults.BaseURL.String())
	assert.Equal(t, 10*time.Second, cfg.Engine.Timeout)
	assert.Equal(t, "FastClient/1.0", cfg.Defaults.Headers.Get("User-Agent"))
	assert.Equal(t, "FastApp", cfg.Defaults.Headers.Get("X-Custom-App"))
}

func TestFastClient_With(t *testing.T) {
	t.Parallel()

	baseClient := fast.NewClient(
		option.WithBaseURL("http://example.com/v1"),
		option.WithTimeout(10*time.Second),
	)

	clonedClient := baseClient.With(
		option.WithUserAgent("ClonedFastClient/1.0"),
		option.WithHeader("X-Cloned", "true"),
	)

	cfg1 := baseClient.Config()
	cfg2 := clonedClient.Config()

	assert.Equal(t, "", cfg1.Defaults.Headers.Get("User-Agent"))
	assert.Equal(t, "ClonedFastClient/1.0", cfg2.Defaults.Headers.Get("User-Agent"))
	assert.Equal(t, "true", cfg2.Defaults.Headers.Get("X-Cloned"))
}

func TestFastClient_MiddlewareChain(t *testing.T) {
	t.Parallel()

	var attempts int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer ts.Close()

	fastClient := fast.NewClient(option.WithTimeout(5 * time.Second))

	chained := middleware.Chain(
		fastClient,
		middleware.Retry(
			middleware.RetryOptions{MaxRetries: 3, Backoff: 1 * time.Millisecond},
			middleware.RetryOnGatewayErrors(),
		),
		middleware.RateLimit(100, 10),
	)

	req := fast.NewRequest(h1engine.AcquireRequest())
	t.Cleanup(req.Release)

	req.SetURL(ts.URL + "/test")
	req.SetMethod(http.MethodGet)

	resp, err := chained.Do(req)
	require.NoError(t, err)

	t.Cleanup(func() { _ = resp.Close() })

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, []byte(`{"success":true}`), resp.BodyBytes())
	assert.Equal(t, 3, attempts)
}

func TestFast_Resiliency_RetryAfterBackoff(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := attempts.Add(1)
		if count == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, "rate limited")

			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "rate limit recovered ok")
	}))
	t.Cleanup(ts.Close)

	fastClient := fast.NewClient()

	chained := middleware.Chain(
		fastClient,
		middleware.Retry(
			middleware.RetryOptions{
				MaxRetries: 3,
				Backoff:    10 * time.Millisecond,
			},
			middleware.RetryOnRateLimit(),
		),
	)

	req := fast.NewRequest(nil)
	t.Cleanup(req.Release)

	req.SetURL(ts.URL)
	req.SetMethod(http.MethodGet)

	resp, err := chained.Do(req)
	require.NoError(t, err)

	t.Cleanup(func() { _ = resp.Close() })

	assert.Equal(t, http.StatusOK, resp.StatusCode())
	assert.Equal(t, "rate limit recovered ok", string(resp.BodyBytes()))
	assert.Equal(t, int32(2), attempts.Load())
}

func TestFast_Resiliency_CircuitBreaker(t *testing.T) {
	t.Parallel()

	var serverHits atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverHits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)

	cb := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
		Cooldown:         5 * time.Second,
		MinRequests:      2,
		FailureThreshold: 0.5,
	})

	fastClient := fast.NewClient()
	cbClient := middleware.CircuitBreak(cb, nil)(fastClient)

	// Request 1 -> 500 Server Error
	req1 := fast.NewRequest(nil)
	req1.SetURL(ts.URL)
	req1.SetMethod(http.MethodGet)

	resp1, _ := cbClient.Do(req1)
	if resp1 != nil {
		_ = resp1.Close()
	}

	// Request 2 -> 500 Server Error (Trips Circuit Breaker)
	req2 := fast.NewRequest(nil)
	req2.SetURL(ts.URL)
	req2.SetMethod(http.MethodGet)

	resp2, _ := cbClient.Do(req2)
	if resp2 != nil {
		_ = resp2.Close()
	}

	// Request 3 -> Should fail immediately with circuit open error without hitting server
	hitsBefore := serverHits.Load()

	req3 := fast.NewRequest(nil)
	req3.SetURL(ts.URL)
	req3.SetMethod(http.MethodGet)

	resp3, err3 := cbClient.Do(req3)
	if resp3 != nil {
		_ = resp3.Close()
	}

	require.Error(t, err3)
	assert.Contains(t, err3.Error(), "circuit breaker open")
	assert.Equal(t, hitsBefore, serverHits.Load())
}

func TestFast_Resiliency_LoadBalancer(t *testing.T) {
	t.Parallel()

	s1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Server-Id", "server-1")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "server 1")
	}))
	t.Cleanup(s1.Close)

	s2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Server-Id", "server-2")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "server 2")
	}))
	t.Cleanup(s2.Close)

	c1 := fast.NewStdClient(fast.NewClient())
	c2 := fast.NewStdClient(fast.NewClient())

	balancer, err := loadbalancer.New(loadbalancer.Config{
		Strategy: loadbalancer.RoundRobin,
	}, s1.URL, s2.URL)
	require.NoError(t, err)

	balancer.WithClients(c1, c2)

	hits := make(map[string]int)
	for range 4 {
		req, reqErr := http.NewRequestWithContext(t.Context(), http.MethodGet, s1.URL, nil)
		require.NoError(t, reqErr)

		resp, doErr := balancer.Do(req)
		require.NoError(t, doErr)

		serverID := resp.Header.Get("X-Server-Id")
		hits[serverID]++

		_ = resp.Body.Close()
	}

	assert.Equal(t, 2, hits["server-1"])
	assert.Equal(t, 2, hits["server-2"])
	assert.NoError(t, balancer.Close())
}

func TestFast_ScopedBorrow(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "aoni-borrow-test")
		w.Header().Set("Set-Cookie", "auth_token=secret_12345; Path=/")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"zero-alloc-borrow","status":"ok"}`))
	}))
	t.Cleanup(ts.Close)

	client := fast.NewClient()
	req := client.AcquireRequest()
	defer client.ReleaseRequest(req)

	req.SetURL(ts.URL)
	req.SetMethod("GET")

	scope := borrow.AcquireScope()
	defer scope.Release()

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Close()

	var (
		headerVal borrow.Bytes
		cookieVal borrow.Bytes
		bodyVal   borrow.Bytes
	)

	if pr, isPooled := resp.(*fast.PooledResponse); isPooled {
		headerVal = pr.HeaderScoped(scope, "X-Custom-Header")
		cookieVal = pr.CookieScoped(scope, "auth_token")
		bodyVal = pr.BodyScoped(scope)
	} else if fr, isDirect := resp.(*fast.Response); isDirect {
		headerVal = fr.HeaderScoped(scope, "X-Custom-Header")
		cookieVal = fr.CookieScoped(scope, "auth_token")
		bodyVal = fr.BodyScoped(scope)
	} else {
		t.Fatalf("unexpected response type: %T", resp)
	}

	assert.Equal(t, "aoni-borrow-test", string(headerVal.AsSlice()))
	assert.Equal(t, "secret_12345", string(cookieVal.AsSlice()))
	assert.Equal(t, `{"message":"zero-alloc-borrow","status":"ok"}`, string(bodyVal.AsSlice()))

	// Test ReadBodyScoped
	var captured string
	var readErr error
	if pr, isPooled := resp.(*fast.PooledResponse); isPooled {
		readErr = pr.ReadBodyScoped(func(b []byte) error {
			captured = string(b)
			return nil
		})
	} else if fr, isDirect := resp.(*fast.Response); isDirect {
		readErr = fr.ReadBodyScoped(func(b []byte) error {
			captured = string(b)
			return nil
		})
	}
	require.NoError(t, readErr)
	assert.Equal(t, `{"message":"zero-alloc-borrow","status":"ok"}`, captured)
}
