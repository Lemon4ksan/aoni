// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttputil"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/request"
)

type fastBenchUser struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func setupFastBenchServer() (*fasthttputil.InmemoryListener, *fasthttp.Server) {
	ln := fasthttputil.NewInmemoryListener()
	srv := &fasthttp.Server{
		Handler: func(ctx *fasthttp.RequestCtx) {
			ctx.SetContentType("application/json")
			ctx.SetStatusCode(fasthttp.StatusOK)
			ctx.SetBodyString(`{"id":42,"name":"Benchmark User","email":"bench@aoni.dev"}`)
		},
	}

	go func() {
		_ = srv.Serve(ln)
	}()

	return ln, srv
}

func BenchmarkGET_JSON_FastClient(b *testing.B) {
	ln, srv := setupFastBenchServer()
	defer func() {
		_ = srv.Shutdown()
		_ = ln.Close()
	}()

	fastClient := fast.NewClient(
		option.WithBaseURL("http://inmemory"),
		option.WithTimeout(5*time.Second),
	)

	fastClient.Engine().Dial = func(_ string) (net.Conn, error) {
		return ln.Dial()
	}

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var user fastBenchUser
		resp, err := fastClient.Request(ctx, "GET", "/user")
		if err != nil {
			b.Fatalf("fast request failed: %v", err)
		}

		if err := json.Unmarshal(resp.BodyBytes(), &user); err != nil {
			_ = resp.Close()
			b.Fatalf("decode failed: %v", err)
		}
		_ = resp.Close()

		if user.ID != 42 {
			b.Fatalf("expected ID 42, got %d", user.ID)
		}
	}
}

func BenchmarkGET_JSON_StdClient(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":42,"name":"Benchmark User","email":"bench@aoni.dev"}`))
	}))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithBaseURL(server.URL))
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var user fastBenchUser
		err := request.GetInto(ctx, client, "/", &user)
		if err != nil {
			b.Fatalf("std request failed: %v", err)
		}

		if user.ID != 42 {
			b.Fatalf("expected ID 42, got %d", user.ID)
		}
	}
}

func BenchmarkPOST_JSON_FastClient(b *testing.B) {
	ln, srv := setupFastBenchServer()
	defer func() {
		_ = srv.Shutdown()
		_ = ln.Close()
	}()

	fastClient := fast.NewClient(
		option.WithBaseURL("http://inmemory"),
		option.WithTimeout(5*time.Second),
	)

	fastClient.Engine().Dial = func(_ string) (net.Conn, error) {
		return ln.Dial()
	}

	ctx := context.Background()
	payload := fastBenchUser{ID: 100, Name: "Post User", Email: "post@aoni.dev"}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var respUser fastBenchUser
		resp, err := fastClient.Request(ctx, "POST", "/user", mod.WithJSONBody(payload))
		if err != nil {
			b.Fatalf("fast post failed: %v", err)
		}

		if err := json.Unmarshal(resp.BodyBytes(), &respUser); err != nil {
			_ = resp.Close()
			b.Fatalf("decode failed: %v", err)
		}
		_ = resp.Close()
	}
}

func BenchmarkModifiers_FastVsStd(b *testing.B) {
	b.Run("FastRequest_Adapter", func(b *testing.B) {
		fastReq := fasthttp.AcquireRequest()
		defer fasthttp.ReleaseRequest(fastReq)
		fastReq.SetRequestURI("http://api.example.com/v1/resource")

		req := fast.NewRequest(fastReq)
		modifier := mod.WithHeader("X-Benchmark-Header", "aoni-fast")

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			modifier(req)
		}
	})

	b.Run("StdRequest_Adapter", func(b *testing.B) {
		httpReq, _ := http.NewRequest(http.MethodGet, "http://api.example.com/v1/resource", nil)
		req := aoni.NewStdRequest(httpReq)
		modifier := mod.WithHeader("X-Benchmark-Header", "aoni-std")

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			modifier(req)
		}
	})
}

func BenchmarkGET_JSON_Standard_NetHTTP(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":42,"name":"Benchmark User","email":"bench@aoni.dev"}`))
	}))
	defer server.Close()

	client := server.Client()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}

		var user fastBenchUser
		err = json.NewDecoder(resp.Body).Decode(&user)
		_ = resp.Body.Close()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGET_JSON_Resty_FastBridged(b *testing.B) {
	ln, srv := setupFastBenchServer()
	defer func() {
		_ = srv.Shutdown()
		_ = ln.Close()
	}()

	fastClient := fast.NewClient(
		option.WithBaseURL("http://inmemory"),
		option.WithTimeout(5*time.Second),
	)
	fastClient.Engine().Dial = func(_ string) (net.Conn, error) {
		return ln.Dial()
	}

	stdHTTPClient := fast.NewStdClient(fastClient)
	restyClient := resty.NewWithClient(stdHTTPClient)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var user fastBenchUser
		resp, err := restyClient.R().SetContext(ctx).SetResult(&user).Get("http://inmemory/user")
		if err != nil {
			b.Fatalf("resty fast request failed: %v", err)
		}

		if resp.StatusCode() != http.StatusOK || user.ID != 42 {
			b.Fatalf("invalid resty fast response: %v", resp.StatusCode())
		}
	}
}

func BenchmarkGET_JSON_Aoni_FastBridged(b *testing.B) {
	ln, srv := setupFastBenchServer()
	defer func() {
		_ = srv.Shutdown()
		_ = ln.Close()
	}()

	fastClient := fast.NewClient(
		option.WithBaseURL("http://inmemory"),
		option.WithTimeout(5*time.Second),
	)
	fastClient.Engine().Dial = func(_ string) (net.Conn, error) {
		return ln.Dial()
	}

	stdHTTPClient := fast.NewStdClient(fastClient)
	aoniClient := aoni.NewClient(stdHTTPClient, option.WithBaseURL("http://inmemory"))
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var user fastBenchUser
		err := request.GetInto(ctx, aoniClient, "/user", &user)
		if err != nil {
			b.Fatalf("aoni fast bridged request failed: %v", err)
		}

		if user.ID != 42 {
			b.Fatalf("invalid aoni fast response: %d", user.ID)
		}
	}
}

func BenchmarkGET_FastClient_Parallel(b *testing.B) {
	ln, srv := setupFastBenchServer()
	defer func() {
		_ = srv.Shutdown()
		_ = ln.Close()
	}()

	fastClient := fast.NewClient(
		option.WithBaseURL("http://inmemory"),
		option.WithTimeout(5*time.Second),
	)
	fastClient.Engine().Dial = func(_ string) (net.Conn, error) {
		return ln.Dial()
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := fastClient.Request(ctx, "GET", "/user")
			if err != nil {
				b.Fatalf("fast parallel request failed: %v", err)
			}
			_ = resp.Close()
		}
	})
}
