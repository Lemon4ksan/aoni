// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni/fast"
)

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

	c := &fasthttp.Client{}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		req := fasthttp.AcquireRequest()
		resp := fasthttp.AcquireResponse()

		req.SetRequestURI(ts.URL)

		for pb.Next() {
			if err := c.Do(req, resp); err != nil {
				b.Fatalf("fasthttp do failed: %v", err)
			}

			req.Reset()
			resp.Reset()
			req.SetRequestURI(ts.URL)
		}

		fasthttp.ReleaseRequest(req)
		fasthttp.ReleaseResponse(resp)
	})
}

func TestLatencyProfile_Fast_Vs_StdHTTP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"ok","engine":"fast"}`)
	}))
	defer ts.Close()

	const (
		concurrency   = 100
		reqsPerWorker = 300
		totalReqs     = concurrency * reqsPerWorker
	)

	// 1. Profile aoni/fast
	fastClient := fast.NewClient()
	fastLatencies := make([]time.Duration, totalReqs)

	var fastIdx int32

	var wg sync.WaitGroup

	fastStart := time.Now()

	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			ctx := context.Background()
			for range reqsPerWorker {
				t0 := time.Now()
				resp, err := fastClient.Request(ctx, "GET", ts.URL)
				elapsed := time.Since(t0)

				require.NoError(t, err)
				resp.Close()

				idx := atomic.AddInt32(&fastIdx, 1) - 1
				fastLatencies[idx] = elapsed
			}
		}()
	}

	wg.Wait()

	fastDuration := time.Since(fastStart)

	// 2. Profile std net/http
	stdClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: concurrency,
		},
	}
	stdLatencies := make([]time.Duration, totalReqs)

	var stdIdx int32

	stdStart := time.Now()
	for range concurrency {
		wg.Go(func() {
			for range reqsPerWorker {
				t0 := time.Now()
				req, err := http.NewRequestWithContext(context.Background(), "GET", ts.URL, nil)
				require.NoError(t, err)
				resp, err := stdClient.Do(req)
				require.NoError(t, err)

				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				elapsed := time.Since(t0)

				idx := atomic.AddInt32(&stdIdx, 1) - 1
				stdLatencies[idx] = elapsed
			}
		})
	}

	wg.Wait()

	stdDuration := time.Since(stdStart)

	// 3. Compute metrics
	sort.Slice(fastLatencies, func(i, j int) bool { return fastLatencies[i] < fastLatencies[j] })
	sort.Slice(stdLatencies, func(i, j int) bool { return stdLatencies[i] < stdLatencies[j] })

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
