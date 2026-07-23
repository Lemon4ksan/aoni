// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build bench_resty

package aoni_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fluent"
	"github.com/lemon4ksan/aoni/option"
)

type benchUser struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func setupBenchClient(server *httptest.Server) *http.Client {
	if tr, ok := server.Client().Transport.(*http.Transport); ok {
		tr.MaxIdleConns = 10000
		tr.MaxIdleConnsPerHost = 10000
		return &http.Client{Transport: tr}
	}

	return server.Client()
}

func setupBenchServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":42,"name":"Benchmark User","email":"bench@aoni.dev"}`))
	}))
}

func BenchmarkGET_Aoni(b *testing.B) {
	server := setupBenchServer()
	defer server.Close()

	client := aoni.NewClient(setupBenchClient(server), option.WithBaseURL(server.URL))
	ctx := b.Context()

	b.ReportAllocs()

	for b.Loop() {
		var u benchUser

		_, err := fluent.R(client).
			SetContext(ctx).
			SetResult(&u).
			Get("/")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGET_Resty(b *testing.B) {
	server := setupBenchServer()
	defer server.Close()

	client := resty.NewWithClient(setupBenchClient(server)).SetBaseURL(server.URL)
	ctx := b.Context()

	b.ReportAllocs()

	for b.Loop() {
		var u benchUser

		_, err := client.R().
			SetContext(ctx).
			SetResult(&u).
			Get("/")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGET_Parallel_Aoni(b *testing.B) {
	server := setupBenchServer()
	defer server.Close()

	client := aoni.NewClient(setupBenchClient(server), option.WithBaseURL(server.URL))
	ctx := b.Context()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var u benchUser

			_, err := fluent.R(client).
				SetContext(ctx).
				SetResult(&u).
				Get("/")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkGET_Parallel_Resty(b *testing.B) {
	server := setupBenchServer()
	defer server.Close()

	client := resty.NewWithClient(setupBenchClient(server)).SetBaseURL(server.URL)
	ctx := b.Context()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			var u benchUser

			_, err := client.R().
				SetContext(ctx).
				SetResult(&u).
				Get("/")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkBuilderCreation_Aoni(b *testing.B) {
	client := aoni.NewClient(nil)

	b.ReportAllocs()

	for b.Loop() {
		req := fluent.R(client).
			SetHeader("X-Custom", "test").
			SetQueryParam("page", "1").
			SetBearerToken("token_123")

		req.Release()
	}
}

func BenchmarkBuilderCreation_Resty(b *testing.B) {
	client := resty.New()

	b.ReportAllocs()

	for b.Loop() {
		_ = client.R().
			SetHeader("X-Custom", "test").
			SetQueryParam("page", "1").
			SetAuthToken("token_123")
	}
}
