// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/internal/fast/h1engine"

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

func setupFastBenchServer() *h1engine.InmemoryListener {
	ln := h1engine.NewInmemoryListener()
	respBytes := []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 58\r\nConnection: keep-alive\r\n\r\n{\"id\":42,\"name\":\"Benchmark User\",\"email\":\"bench@aoni.dev\"}")

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				buf := make([]byte, 1024)
				for {
					n, err := conn.Read(buf)
					if err != nil || n == 0 {
						return
					}
					if _, err := conn.Write(respBytes); err != nil {
						return
					}
				}
			}(c)
		}
	}()

	return ln
}

func BenchmarkClient_Get_Fast(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "aoni fast benchmark payload")
	}))
	defer ts.Close()

	c := fast.NewClient()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			resp, err := c.Request(ctx, "GET", ts.URL)
			if err != nil {
				b.Fatalf("fast request failed: %v", err)
			}

			if resp.StatusCode() != http.StatusOK {
				b.Fatalf("unexpected status: %d", resp.StatusCode())
			}

			resp.Close()
		}
	})
}

func BenchmarkClient_Get_StdHTTP(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "aoni fast benchmark payload")
	}))
	defer ts.Close()

	c := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 100,
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req, err := http.NewRequestWithContext(context.Background(), "GET", ts.URL, nil)
			if err != nil {
				b.Fatalf("std new request failed: %v", err)
			}

			resp, err := c.Do(req)
			if err != nil {
				b.Fatalf("std request failed: %v", err)
			}

			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	})
}

func BenchmarkClient_Get_RawFastHTTP(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "aoni fast benchmark payload")
	}))
	defer ts.Close()

	c := &h1engine.Client{}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		req := h1engine.AcquireRequest()
		resp := h1engine.AcquireResponse()

		req.SetRequestURI(ts.URL)

		for pb.Next() {
			if err := c.Do(req, resp); err != nil {
				b.Fatalf("fasthttp do failed: %v", err)
			}

			req.Reset()
			resp.Reset()
			req.SetRequestURI(ts.URL)
		}

		h1engine.ReleaseRequest(req)
		h1engine.ReleaseResponse(resp)
	})
}

func BenchmarkClient_Get_BridgeStdClient(b *testing.B) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "aoni fast benchmark payload")
	}))
	b.Cleanup(ts.Close)

	c := fast.NewClient()
	stdClient := fast.NewStdClient(c)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := stdClient.Get(ts.URL)
			if err != nil {
				b.Fatalf("std bridge request failed: %v", err)
			}

			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	})
}

func BenchmarkFastAdapter_ZeroAllocations(b *testing.B) {
	fastReq := h1engine.AcquireRequest()
	defer h1engine.ReleaseRequest(fastReq)

	fastReq.SetRequestURI("http://api.example.com/v1/users")

	key := []byte("Authorization")
	val := []byte("Bearer secret-token-12345")
	queryKey := []byte("page")
	queryVal := []byte("10")

	req := fast.NewRequest(fastReq)

	b.ReportAllocs()

	for b.Loop() {
		req.SetHeaderBytes(key, val)
		req.SetQueryParamBytes(queryKey, queryVal)
		_ = req.HeaderBytes(key)
	}
}

func BenchmarkGET_JSON_FastClient(b *testing.B) {
	ln := setupFastBenchServer()
	defer ln.Close()

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

func BenchmarkGET_JSON_FastClient_ZeroCopy(b *testing.B) {
	ln := setupFastBenchServer()
	defer ln.Close()

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

		if pr, ok := resp.(*fast.PooledResponse); ok {
			_ = pr.JSONNoCopy(&user)
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
	ln := setupFastBenchServer()
	defer ln.Close()

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
		fastReq := h1engine.AcquireRequest()
		defer h1engine.ReleaseRequest(fastReq)
		fastReq.SetRequestURI("http://api.example.com/v1/resource")

		req := fast.NewRequest(fastReq)
		modifier := mod.WithHeader("X-Benchmark-Header", "aoni-fast")

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			modifier.Apply(req)
		}
	})

	b.Run("StdRequest_Adapter", func(b *testing.B) {
		httpReq, _ := http.NewRequest(http.MethodGet, "http://api.example.com/v1/resource", nil)
		req := aoni.NewStdRequest(httpReq)
		modifier := mod.WithHeader("X-Benchmark-Header", "aoni-std")

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			modifier.Apply(req)
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
		user, err := netHTTPGetTo[fastBenchUser](ctx, client, server.URL)
		if err != nil {
			b.Fatal(err)
		}

		if user.ID != 42 {
			b.Fatalf("expected ID 42, got %d", user.ID)
		}
	}
}

func BenchmarkGET_JSON_Aoni_FastBridged(b *testing.B) {
	ln := setupFastBenchServer()
	defer ln.Close()

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
	ln := setupFastBenchServer()
	defer ln.Close()

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

func BenchmarkPOST_FastClient_Parallel(b *testing.B) {
	ln := setupFastBenchServer()
	defer ln.Close()

	fastClient := fast.NewClient(
		option.WithBaseURL("http://inmemory"),
		option.WithTimeout(5*time.Second),
	)
	fastClient.Engine().Dial = func(_ string) (net.Conn, error) {
		return ln.Dial()
	}
	ctx := context.Background()
	payload := []byte(`{"message":"hello world from vectored io benchmark"}`)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := fastClient.Request(ctx, "POST", "/submit", mod.WithBodyBytes(payload))
			if err != nil {
				b.Fatalf("fast post request failed: %v", err)
			}
			_ = resp.Close()
		}
	})
}

func BenchmarkPOST_FastClient_Native_Parallel(b *testing.B) {
	ln := setupFastBenchServer()
	defer ln.Close()

	engine := &h1engine.HostClient{
		Addr: "inmemory",
		Dial: func(_ string) (net.Conn, error) {
			return ln.Dial()
		},
	}

	payload := []byte(`{"message":"hello world from vectored io benchmark"}`)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		req := h1engine.AcquireRequest()
		resp := h1engine.AcquireResponse()
		defer h1engine.ReleaseRequest(req)
		defer h1engine.ReleaseResponse(resp)

		req.Header.SetMethod("POST")
		req.SetRequestURI("http://inmemory/submit")
		req.SetBody(payload)

		for pb.Next() {
			if err := engine.Do(req, resp); err != nil {
				b.Fatalf("fast native post failed: %v", err)
			}
		}
	})
}
