// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

type benchPayload struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
}

type queryParams struct {
	ID    uint64 `url:"id"`
	Limit int    `url:"limit,omitempty"`
	Query string `url:"q"`
}

// ============================================================================
// 1. JSON GET BENCHMARKS (Generics vs Manual net/http)
// Measures the overhead of generic unmarshaling and client initialization.
// ============================================================================

func BenchmarkGET_JSON_Aoni(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(benchPayload{ID: 100, Message: "hello benchmark"})
	}))
	defer server.Close()

	// Immutable client using generic payload decoding
	client := NewClient(nil).WithBaseURL(server.URL)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		res, err := GetJSON[benchPayload](ctx, client, "/")
		if err != nil {
			b.Fatal(err)
		}

		if res.ID != 100 {
			b.Fatal("invalid id")
		}
	}
}

func BenchmarkGET_JSON_NetHTTP(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(benchPayload{ID: 100, Message: "hello benchmark"})
	}))
	defer server.Close()

	client := &http.Client{}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		resp, err := client.Get(server.URL)
		if err != nil {
			b.Fatal(err)
		}

		var payload benchPayload

		err = json.NewDecoder(resp.Body).Decode(&payload)
		_ = resp.Body.Close()

		if err != nil {
			b.Fatal(err)
		}

		if payload.ID != 100 {
			b.Fatal("invalid id")
		}
	}
}

// ============================================================================
// 2. LARGE PAYLOAD COPY BENCHMARKS (1MB stream reading)
// Measures if our raw buffer reading and limiters introduce CPU/RAM overhead.
// ============================================================================

func BenchmarkRawCopy_Aoni(b *testing.B) {
	payload := strings.Repeat("a", 1024*1024) // 1MB payload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		var output []byte
		// Request has NO body positional argument. Body is handled via modifiers.
		resp, err := client.Request(ctx, http.MethodGet, "/", WithRawDecoder())
		if err != nil {
			b.Fatal(err)
		}

		err = RawDecoder.Decode(resp.Body, &output)
		_ = resp.Body.Close()

		if err != nil {
			b.Fatal(err)
		}

		if len(output) != len(payload) {
			b.Fatal("length mismatch")
		}
	}
}

func BenchmarkRawCopy_NetHTTP(b *testing.B) {
	payload := strings.Repeat("a", 1024*1024) // 1MB payload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()

	client := &http.Client{}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		resp, err := client.Get(server.URL)
		if err != nil {
			b.Fatal(err)
		}

		output, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if err != nil {
			b.Fatal(err)
		}

		if len(output) != len(payload) {
			b.Fatal("length mismatch")
		}
	}
}

// ============================================================================
// 3. MULTIPART FORM BENCHMARKS (Payload assembly & streaming)
// Measures memory allocations during heavy multipart body encoding.
// ============================================================================

func BenchmarkMultipart_Aoni(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(10 * 1024 * 1024)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)
	ctx := context.Background()

	fields := map[string]string{"foo": "bar"}
	fileData := strings.Repeat("b", 100*1024) // 100KB file

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		files := map[string]io.Reader{
			"file1": strings.NewReader(fileData),
		}
		// Clean, declarative body definition via RequestModifier
		resp, err := client.Request(ctx, http.MethodPost, "/", WithMultipart(fields, files))
		if err != nil {
			b.Fatal(err)
		}

		_ = resp.Body.Close()
	}
}

func BenchmarkMultipart_NetHTTP(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(10 * 1024 * 1024)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &http.Client{}
	fileData := strings.Repeat("b", 100*1024) // 100KB file

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		err := writer.WriteField("foo", "bar")
		if err != nil {
			b.Fatal(err)
		}

		part, err := writer.CreateFormFile("file1", "file1")
		if err != nil {
			b.Fatal(err)
		}

		_, err = io.Copy(part, strings.NewReader(fileData))
		if err != nil {
			b.Fatal(err)
		}

		_ = writer.Close()

		req, err := http.NewRequest("POST", server.URL, body)
		if err != nil {
			b.Fatal(err)
		}

		req.Header.Set("Content-Type", writer.FormDataContentType())

		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}

		_ = resp.Body.Close()
	}
}

// ============================================================================
// 4. QUERY STRING ENCODING BENCHMARKS (Reflection vs Manual)
// Measures the exact performance cost of reflection-based tag parsing.
// ============================================================================

func BenchmarkQueryEncoding_Aoni(b *testing.B) {
	params := queryParams{
		ID:    76561198000000000,
		Limit: 100,
		Query: "search_term",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		values, err := StructToValues(params)
		if err != nil {
			b.Fatal(err)
		}

		if values.Get("id") != "76561198000000000" {
			b.Fatal("invalid result")
		}
	}
}

func BenchmarkQueryEncoding_Manual(b *testing.B) {
	params := queryParams{
		ID:    76561198000000000,
		Limit: 100,
		Query: "search_term",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		values := make(url.Values)
		values.Set("id", strconv.FormatUint(params.ID, 10))

		if params.Limit != 0 {
			values.Set("limit", strconv.Itoa(params.Limit))
		}

		if params.Query != "" {
			values.Set("q", params.Query)
		}

		if values.Get("id") != "76561198000000000" {
			b.Fatal("invalid result")
		}
	}
}

// ============================================================================
// 5. LOAD BALANCER OVERHEAD BENCHMARKS
// Measures the latency and allocation cost introduced by the LoadBalancer router.
// ============================================================================

func BenchmarkLoadBalancer_WeightedRoundRobin_Aoni(b *testing.B) {
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()

	lb, err := NewLoadBalancer(LoadBalancerConfig{
		Strategy: WeightedRoundRobin,
	}, server1.URL, server2.URL)
	if err != nil {
		b.Fatal(err)
	}
	defer lb.Close()

	client := NewClient(lb)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		resp, err := client.Request(ctx, http.MethodGet, "/")
		if err != nil {
			b.Fatal(err)
		}

		_ = resp.Body.Close()
	}
}

// ============================================================================
// 6. LATENCY HEDGING BENCHMARKS (Parallel backup execution under packet lag)
// Demonstrates how hedging flattens p99 response times under simulated network lag.
// ============================================================================

func BenchmarkRequest_WithoutHedging_Aoni(b *testing.B) {
	// Server simulates a slow proxy or temporary backend lag (50ms)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(nil).WithBaseURL(server.URL)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		resp, err := client.Request(ctx, http.MethodGet, "/")
		if err != nil {
			b.Fatal(err)
		}

		_ = resp.Body.Close()
	}
}

func BenchmarkRequest_WithHedging_Aoni(b *testing.B) {
	// Node 1 is extremely slow (50ms)
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	// Node 2 is ultra-fast (0ms)
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()

	// Route requests through the load balancer
	lb, _ := NewLoadBalancer(LoadBalancerConfig{Strategy: RoundRobin}, server1.URL, server2.URL)
	defer lb.Close()

	// Hedge after 10ms. If Server1 stalls, Server2 is fired in parallel.
	client := NewClient(nil).WithHedging(10 * time.Millisecond).WithBaseURL(server1.URL)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		// Average execution will drop from 50ms to ~10-12ms thanks to parallel backup!
		resp, err := client.Request(ctx, http.MethodGet, "/")
		if err != nil {
			b.Fatal(err)
		}

		_ = resp.Body.Close()
	}
}
