// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttputil"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/middleware"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
)

func TestFastBridge_StdClient(t *testing.T) {
	t.Parallel()

	ln := fasthttputil.NewInmemoryListener()
	srv := &fasthttp.Server{
		Handler: func(ctx *fasthttp.RequestCtx) {
			ctx.SetStatusCode(fasthttp.StatusOK)
			ctx.Response.Header.Set("X-Bridge-Engine", "fasthttp")
			ctx.SetBodyString(`{"bridged":true}`)
		},
	}

	go func() {
		_ = srv.Serve(ln)
	}()

	t.Cleanup(func() {
		_ = srv.Shutdown()
		_ = ln.Close()
	})

	fastClient := fast.NewClient(option.WithTimeout(5 * time.Second))
	fastClient.Engine().Dial = func(_ string) (net.Conn, error) {
		return ln.Dial()
	}

	// Wrap fastClient into a standard *http.Client
	stdClient := fast.NewStdClient(fastClient)

	req, err := http.NewRequestWithContext(context.Background(), "GET", "http://inmemory/test", nil)
	require.NoError(t, err)

	resp, err := stdClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "fasthttp", resp.Header.Get("X-Bridge-Engine"))

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

func TestFastRequest_Contract(t *testing.T) {
	t.Parallel()

	fastReq := fasthttp.AcquireRequest()
	t.Cleanup(func() { fasthttp.ReleaseRequest(fastReq) })

	req := fast.NewRequest(fastReq)
	require.NotNil(t, req.FastHTTPRequest())
	assert.Nil(t, req.HTTPRequest())
	assert.Same(t, fastReq, req.EngineRequest())

	req.SetURL("http://example.com/api/v1?k=v")
	assert.Equal(t, "GET", req.Method())

	req.SetMethod("POST")
	assert.Equal(t, "POST", req.Method())

	req.SetMethodBytes([]byte("PATCH"))
	assert.Equal(t, "PATCH", req.Method())

	assert.Equal(t, "/api/v1", req.Path())
	req.SetPath("/api/v2")
	assert.Equal(t, "/api/v2", req.Path())

	// Query params
	req.AddQueryParam("p1", "v1")
	req.AddQueryParamBytes([]byte("p2"), []byte("v2"))
	assert.Contains(t, req.RawQuery(), "p1=v1")
	assert.Contains(t, req.RawQuery(), "p2=v2")

	req.SetQueryParam("p1", "updated")
	assert.Contains(t, req.RawQuery(), "p1=updated")

	// Headers (zero-alloc byte operations)
	req.SetHeader("X-Header-1", "val1")
	req.SetHeaderBytes([]byte("X-Header-2"), []byte("val2"))

	assert.Equal(t, "val1", req.Header("X-Header-1"))
	assert.Equal(t, "val2", string(req.HeaderBytes([]byte("X-Header-2"))))

	req.DelHeaderBytes([]byte("X-Header-1"))
	assert.Empty(t, req.Header("X-Header-1"))

	// Body
	bodyPayload := []byte(`{"key": "fast_value"}`)
	req.SetBodyBytes(bodyPayload)
	assert.Equal(t, bodyPayload, req.BodyBytes())

	// Reset headers
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

	fastReq := fasthttp.AcquireRequest()
	t.Cleanup(func() { fasthttp.ReleaseRequest(fastReq) })
	fastReq.SetRequestURI("http://localhost/test")

	fReq := fast.NewRequest(fastReq)
	for _, m := range modifiers {
		m(fReq)
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

	fastRespStruct := fasthttp.AcquireResponse()
	t.Cleanup(func() { fasthttp.ReleaseResponse(fastRespStruct) })
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

func BenchmarkFastAdapter_ZeroAllocations(b *testing.B) {
	fastReq := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(fastReq)
	fastReq.SetRequestURI("http://api.example.com/v1/users")

	key := []byte("Authorization")
	val := []byte("Bearer secret-token-12345")
	queryKey := []byte("page")
	queryVal := []byte("10")

	req := fast.NewRequest(fastReq)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		req.SetHeaderBytes(key, val)
		req.SetQueryParamBytes(queryKey, queryVal)
		_ = req.HeaderBytes(key)
	}
}

func TestFastClient_MiddlewareChain(t *testing.T) {
	t.Parallel()

	var attempts int
	ln := fasthttputil.NewInmemoryListener()
	srv := &fasthttp.Server{
		Handler: func(ctx *fasthttp.RequestCtx) {
			attempts++
			if attempts < 3 {
				ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
				return
			}
			ctx.SetStatusCode(fasthttp.StatusOK)
			ctx.SetBodyString(`{"success":true}`)
		},
	}

	go func() {
		_ = srv.Serve(ln)
	}()

	t.Cleanup(func() {
		_ = srv.Shutdown()
		_ = ln.Close()
	})

	fastClient := fast.NewClient(option.WithTimeout(5 * time.Second))
	fastClient.Engine().Dial = func(_ string) (net.Conn, error) {
		return ln.Dial()
	}

	chained := middleware.Chain(
		fastClient,
		middleware.Retry(
			middleware.RetryOptions{MaxRetries: 3, Backoff: 1 * time.Millisecond},
			middleware.RetryOnGatewayErrors(),
		),
		middleware.RateLimit(100, 10),
	)

	req := fast.NewRequest(fasthttp.AcquireRequest())
	req.SetURL("http://inmemory/test")
	req.SetMethod("GET")

	resp, err := chained.Do(req)
	require.NoError(t, err)
	defer resp.Close()

	assert.Equal(t, 200, resp.StatusCode())
	assert.Equal(t, []byte(`{"success":true}`), resp.BodyBytes())
	assert.Equal(t, 3, attempts)
}
