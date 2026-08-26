// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lemon4ksan/aoni/internal/fast/h1engine"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/decode"
	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	lb "github.com/lemon4ksan/aoni/resiliency/loadbalancer"
)

type benchPayload struct {
	Message string `json:"message"`
	ID      int    `json:"id"`
}

type queryParams struct {
	Query string `query:"q"`
	ID    uint64 `query:"id"`
	Limit int    `query:"limit,omitempty"`
}

// setupInmemoryStdServer starts an in-memory net/http server over h1engine.InmemoryListener
// and returns a pre-configured *http.Client that routes connections to it.
func setupInmemoryStdServer(b *testing.B, handler http.Handler) (*h1engine.InmemoryListener, *http.Client) {
	b.Helper()

	ln := h1engine.NewInmemoryListener()
	srv := &http.Server{Handler: handler}

	go func() { _ = srv.Serve(ln) }()

	b.Cleanup(func() {
		_ = srv.Close()
		_ = ln.Close()
	})

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return ln.Dial()
			},
			MaxIdleConnsPerHost: 64,
		},
	}

	return ln, client
}

// BenchmarkGET_Raw_NetHTTP measures raw stdlib net/http execution over in-memory transport (Do + body drain).
func BenchmarkGET_Raw_NetHTTP(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":100,"message":"hello benchmark"}`))
	})
	_, client := setupInmemoryStdServer(b, handler)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://inmemory/", nil)
		if err != nil {
			b.Fatal(err)
		}

		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

// BenchmarkGET_Raw_Aoni measures raw aoni.Client execution over in-memory transport (c.Request + body drain, baremetal mode).
func BenchmarkGET_Raw_Aoni(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":100,"message":"hello benchmark"}`))
	})
	_, httpClient := setupInmemoryStdServer(b, handler)

	// Wrap as HTTPDoerFunc so aoni accepts it as an opaque HTTPDoer and does not
	// call applyDialers, which would overwrite the custom in-memory DialContext.
	client := aoni.NewClient(aoni.HTTPDoerFunc(httpClient.Do), option.WithBaseURL("http://inmemory"), option.WithBaremetal())
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		resp, err := client.Request(ctx, http.MethodGet, "/")
		if err != nil {
			b.Fatal(err)
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

func netHTTPGetTo[T any](ctx context.Context, client *http.Client, urlStr string) (*T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	mediaType, _, _ := strings.Cut(contentType, ";")
	mediaType = strings.TrimSpace(mediaType)

	result := new(T)

	switch {
	case strings.EqualFold(mediaType, "application/json"):
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported content type: %s", contentType)
	}

	return result, nil
}

// BenchmarkGET_Generic_NetHTTP measures generic net/http response unmarshaling over in-memory transport.
func BenchmarkGET_Generic_NetHTTP(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(benchPayload{ID: 100, Message: "hello benchmark"})
	})
	_, client := setupInmemoryStdServer(b, handler)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		res, err := netHTTPGetTo[benchPayload](ctx, client, "http://inmemory/")
		if err != nil {
			b.Fatal(err)
		}

		if res.ID != 100 {
			b.Fatal("invalid id")
		}
	}
}

// BenchmarkGET_Generic_Aoni measures generic aoni response unmarshaling over in-memory transport via request.GetTo[T] (baremetal mode).
func BenchmarkGET_Generic_Aoni(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(benchPayload{ID: 100, Message: "hello benchmark"})
	})
	_, httpClient := setupInmemoryStdServer(b, handler)

	// Wrap as HTTPDoerFunc so aoni accepts it as an opaque HTTPDoer and does not
	// call applyDialers, which would overwrite the custom in-memory DialContext.
	client := aoni.NewClient(aoni.HTTPDoerFunc(httpClient.Do), option.WithBaseURL("http://inmemory"), option.WithBaremetal())
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		res, err := client.Get[benchPayload](ctx, "/")
		if err != nil {
			b.Fatal(err)
		}
		if res.ID != 1001 {
			b.Fatalf("unexpected id: %d", res.ID)
		}
	}
}

// BenchmarkRawCopy_Aoni measures 1MB stream copy execution using zero-copy pipelines.
func BenchmarkRawCopy_Aoni(b *testing.B) {
	payload := strings.Repeat("a", 1024*1024)
	payloadBytes := []byte(payload)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payloadBytes)
	}))
	defer server.Close()

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithMultiReadDisableDisk(true),
	)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		var output []byte

		resp, err := client.Request(ctx, http.MethodGet, "/", decode.WithRaw())
		if err != nil {
			b.Fatal(err)
		}

		err = decode.RawDecoder.Decode(resp.Body, &output)
		_ = resp.Body.Close()

		if err != nil {
			b.Fatal(err)
		}

		if len(output) != len(payload) {
			b.Fatal("length mismatch")
		}
	}
}

// BenchmarkRawCopy_NetHTTP measures standard net/http 1MB stream copy execution.
func BenchmarkRawCopy_NetHTTP(b *testing.B) {
	payloadBytes := []byte(strings.Repeat("a", 1024*1024))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payloadBytes)
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

		if len(output) != len(payloadBytes) {
			b.Fatal("length mismatch")
		}
	}
}

// BenchmarkMultipart_Aoni measures multipart form body assembly and streaming.
func BenchmarkMultipart_Aoni(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(10 * 1024 * 1024)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithBaseURL(server.URL))
	ctx := context.Background()

	fields := map[string]string{"foo": "bar"}
	fileData := strings.Repeat("b", 100*1024)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		files := map[string]io.Reader{
			"file1": strings.NewReader(fileData),
		}

		resp, err := client.Request(ctx, http.MethodPost, "/", mod.WithMultipart(fields, files))
		if err != nil {
			b.Fatal(err)
		}

		_ = resp.Body.Close()
	}
}

// BenchmarkMultipart_NetHTTP measures manual net/http multipart body encoding.
func BenchmarkMultipart_NetHTTP(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(10 * 1024 * 1024)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &http.Client{}
	fileData := strings.Repeat("b", 100*1024)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		if err := writer.WriteField("foo", "bar"); err != nil {
			b.Fatal(err)
		}

		part, err := writer.CreateFormFile("file1", "file1")
		if err != nil {
			b.Fatal(err)
		}

		if _, err = io.Copy(part, strings.NewReader(fileData)); err != nil {
			b.Fatal(err)
		}

		_ = writer.Close()

		req, err := http.NewRequest(http.MethodPost, server.URL, body)
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

// BenchmarkQueryEncoding_Fast_Aoni measures direct zero-allocation URL query string generation.
func BenchmarkQueryEncoding_Fast_Aoni(b *testing.B) {
	params := queryParams{
		ID:    76561198000000000,
		Limit: 100,
		Query: "search_term",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		qStr, err := values.StructToQueryString(params)
		if err != nil {
			b.Fatal(err)
		}

		if qStr == "" {
			b.Fatal("invalid result")
		}
	}
}

// BenchmarkQueryEncoding_Aoni measures legacy url.Values map construction via reflection.
func BenchmarkQueryEncoding_Aoni(b *testing.B) {
	params := queryParams{
		ID:    76561198000000000,
		Limit: 100,
		Query: "search_term",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		vals, err := values.Encode(params)
		if err != nil {
			b.Fatal(err)
		}

		if vals.Get("id") != "76561198000000000" {
			b.Fatal("invalid result")
		}
	}
}

// BenchmarkQueryEncoding_Manual measures manual url.Values map construction.
func BenchmarkQueryEncoding_Manual(b *testing.B) {
	params := queryParams{
		ID:    76561198000000000,
		Limit: 100,
		Query: "search_term",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		vals := make(url.Values)
		vals.Set("id", strconv.FormatUint(params.ID, 10))

		if params.Limit != 0 {
			vals.Set("limit", strconv.Itoa(params.Limit))
		}

		if params.Query != "" {
			vals.Set("q", params.Query)
		}

		if vals.Get("id") != "76561198000000000" {
			b.Fatal("invalid result")
		}
	}
}

// BenchmarkLoadBalancer_WeightedRoundRobin_Aoni measures load balancer request routing overhead.
func BenchmarkLoadBalancer_WeightedRoundRobin_Aoni(b *testing.B) {
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()

	balancer, err := lb.New(lb.Config{
		Strategy: lb.WeightedRoundRobin,
	}, server1.URL, server2.URL)
	if err != nil {
		b.Fatal(err)
	}
	defer balancer.Close()

	client := aoni.NewClient(balancer)
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

// BenchmarkRequest_WithoutHedging_Aoni measures baseline request execution latency.
func BenchmarkRequest_WithoutHedging_Aoni(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithBaseURL(server.URL))
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

// BenchmarkRequest_WithHedging_Aoni measures parallel hedging backup execution under network lag.
func BenchmarkRequest_WithHedging_Aoni(b *testing.B) {
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()

	lbInstance, _ := lb.New(lb.Config{Strategy: lb.RoundRobin}, server1.URL, server2.URL)
	defer lbInstance.Close()

	client := aoni.NewClient(nil,
		option.WithBaseURL(server1.URL),
		option.WithHedging(10*time.Millisecond),
	)
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

// BenchmarkGET_JSON_Aoni_Minimal measures aoni performance in Baremetal mode.
func BenchmarkGET_JSON_Aoni_Minimal(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(benchPayload{ID: 100, Message: "hello benchmark"})
	}))
	defer server.Close()

	client := aoni.NewClient(nil,
		option.WithBaseURL(server.URL),
		option.WithBaremetal(),
	)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		var payload benchPayload

		err := client.GetInto(ctx, "/", &payload)
		if err != nil {
			b.Fatal(err)
		}

		if payload.ID != 100 {
			b.Fatal("invalid id")
		}
	}
}

// BenchmarkGET_JSON_Aoni_UnsafeDisableFlags measures per-request Unsafe phase bypass without loop allocations.
func BenchmarkGET_JSON_Aoni_UnsafeDisableFlags(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(benchPayload{ID: 100, Message: "hello benchmark"})
	}))
	defer server.Close()

	client := aoni.NewClient(nil, option.WithBaseURL(server.URL))

	ctx := aoni.WithContextModifier(context.Background(),
		mod.WithUnsafeDisableFlags(
			pipeline.FlagChallenge|
				pipeline.FlagValidate|
				pipeline.FlagDecompress|
				pipeline.FlagRotateUA|
				pipeline.FlagRedact|
				pipeline.FlagMultiRead,
		),
	)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		var payload benchPayload

		err := client.GetInto(ctx, "/", &payload)
		if err != nil {
			b.Fatal(err)
		}

		if payload.ID != 100 {
			b.Fatal("invalid id")
		}
	}
}

func BenchmarkGET_JSON_Fast_Aoni_UnsafeDisableFlags(b *testing.B) {
	ln := setupFastBenchServer()
	defer ln.Close()

	client := fast.NewClient(
		option.WithBaseURL("http://inmemory"),
		option.WithBaremetal(),
	)

	client.Engine().Dial = func(_ string) (net.Conn, error) {
		return ln.Dial()
	}

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var user fastBenchUser
		resp, err := client.Request(ctx, "GET", "/user")
		if err != nil {
			b.Fatalf("request failed: %v", err)
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

func BenchmarkGET_JSON_Fast_Aoni_Minimal(b *testing.B) {
	ln := setupFastBenchServer()
	defer ln.Close()

	client := fast.NewClient(
		option.WithBaseURL("http://inmemory"),
		option.WithBaremetal(),
	)

	client.Engine().Dial = func(_ string) (net.Conn, error) {
		return ln.Dial()
	}

	ctx := aoni.WithContextModifier(context.Background(),
		mod.WithUnsafeDisableFlags(
			pipeline.FlagChallenge|
				pipeline.FlagValidate|
				pipeline.FlagDecompress|
				pipeline.FlagRotateUA|
				pipeline.FlagRedact|
				pipeline.FlagMultiRead,
		),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var user fastBenchUser
		resp, err := client.Request(ctx, "GET", "/user")
		if err != nil {
			b.Fatalf("request failed: %v", err)
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
