// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/lemon4ksan/aoni/fast"
	"github.com/lemon4ksan/aoni/tests/testutil"
)

func benchHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
}

// -----------------------------------------------------------------------------
// HTTP/1.1 Benchmarks
// -----------------------------------------------------------------------------

func BenchmarkFast_H1_Sequential(b *testing.B) {
	s := testutil.NewH1Server(b, benchHandler())
	defer s.Close()

	fc := fast.NewClient()
	defer fc.Engine().CloseIdleConnections()

	targetURL := s.URL() + "/bench"

	// Warm up
	req := fast.NewRequest(nil)
	req.SetMethod(http.MethodGet)
	req.SetURL(targetURL)
	resp, err := fc.Do(req)
	if err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	req.Release()
	fast.ReleaseResponse(resp)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := fast.NewRequest(nil)
		req.SetMethod(http.MethodGet)
		req.SetURL(targetURL)
		resp, err := fc.Do(req)
		if err != nil {
			b.Fatalf("request failed: %v", err)
		}
		req.Release()
		fast.ReleaseResponse(resp)
	}
}

func BenchmarkFast_H1_Parallel(b *testing.B) {
	s := testutil.NewH1Server(b, benchHandler())
	defer s.Close()

	fc := fast.NewClient()
	defer fc.Engine().CloseIdleConnections()

	targetURL := s.URL() + "/bench"

	// Warm up
	req := fast.NewRequest(nil)
	req.SetMethod(http.MethodGet)
	req.SetURL(targetURL)
	resp, err := fc.Do(req)
	if err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	req.Release()
	fast.ReleaseResponse(resp)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := fast.NewRequest(nil)
			req.SetMethod(http.MethodGet)
			req.SetURL(targetURL)
			resp, err := fc.Do(req)
			if err != nil {
				b.Fatalf("request failed: %v", err)
			}
			req.Release()
			fast.ReleaseResponse(resp)
		}
	})
}

// -----------------------------------------------------------------------------
// HTTP/2 Benchmarks (Multiplexed)
// -----------------------------------------------------------------------------

func BenchmarkFast_H2_Sequential(b *testing.B) {
	s := testutil.NewH2Server(b, benchHandler())
	defer s.Close()

	fc := fast.NewClient(fast.WithTLSConfig(s.TLSConfig()), fast.WithH2())
	defer fc.Engine().CloseIdleConnections()

	targetURL := s.URL() + "/bench"

	// Warm up
	req := fast.NewRequest(nil)
	req.SetMethod(http.MethodGet)
	req.SetURL(targetURL)
	resp, err := fc.Do(req)
	if err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	req.Release()
	fast.ReleaseResponse(resp)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := fast.NewRequest(nil)
		req.SetMethod(http.MethodGet)
		req.SetURL(targetURL)
		resp, err := fc.Do(req)
		if err != nil {
			b.Fatalf("request failed: %v", err)
		}
		req.Release()
		fast.ReleaseResponse(resp)
	}
}

func BenchmarkFast_H2_Parallel(b *testing.B) {
	s := testutil.NewH2Server(b, benchHandler())
	defer s.Close()

	fc := fast.NewClient(fast.WithTLSConfig(s.TLSConfig()), fast.WithH2())
	defer fc.Engine().CloseIdleConnections()

	targetURL := s.URL() + "/bench"

	// Warm up
	req := fast.NewRequest(nil)
	req.SetMethod(http.MethodGet)
	req.SetURL(targetURL)
	resp, err := fc.Do(req)
	if err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	req.Release()
	fast.ReleaseResponse(resp)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := fast.NewRequest(nil)
			req.SetMethod(http.MethodGet)
			req.SetURL(targetURL)
			resp, err := fc.Do(req)
			if err != nil {
				b.Fatalf("request failed: %v", err)
			}
			req.Release()
			fast.ReleaseResponse(resp)
		}
	})
}

// -----------------------------------------------------------------------------
// HTTP/3 Benchmarks (QUIC Multiplexed with QPACK)
// -----------------------------------------------------------------------------

func BenchmarkFast_H3_Sequential(b *testing.B) {
	s := testutil.NewH3Server(b, benchHandler())
	defer s.Close()

	fc := fast.NewClient(fast.WithTLSConfig(s.TLSConfig()), fast.WithH3())
	defer fc.Engine().CloseIdleConnections()

	targetURL := s.URL() + "/bench"

	// Warm up
	req := fast.NewRequest(nil)
	req.SetMethod(http.MethodGet)
	req.SetURL(targetURL)
	resp, err := fc.Do(req)
	if err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	req.Release()
	fast.ReleaseResponse(resp)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := fast.NewRequest(nil)
		req.SetMethod(http.MethodGet)
		req.SetURL(targetURL)
		resp, err := fc.Do(req)
		if err != nil {
			b.Fatalf("request failed: %v", err)
		}
		req.Release()
		fast.ReleaseResponse(resp)
	}
}

func BenchmarkFast_H3_Parallel(b *testing.B) {
	s := testutil.NewH3Server(b, benchHandler())
	defer s.Close()

	fc := fast.NewClient(fast.WithTLSConfig(s.TLSConfig()), fast.WithH3())
	defer fc.Engine().CloseIdleConnections()

	targetURL := s.URL() + "/bench"

	// Warm up
	req := fast.NewRequest(nil)
	req.SetMethod(http.MethodGet)
	req.SetURL(targetURL)
	resp, err := fc.Do(req)
	if err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	req.Release()
	fast.ReleaseResponse(resp)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := fast.NewRequest(nil)
			req.SetMethod(http.MethodGet)
			req.SetURL(targetURL)
			resp, err := fc.Do(req)
			if err != nil {
				b.Fatalf("request failed: %v", err)
			}
			req.Release()
			fast.ReleaseResponse(resp)
		}
	})
}

// -----------------------------------------------------------------------------
// Standard Library Baseline (net/http)
// -----------------------------------------------------------------------------

func BenchmarkStd_H1_Parallel(b *testing.B) {
	s := testutil.NewH1Server(b, benchHandler())
	defer s.Close()

	client := s.Client()
	targetURL := s.URL() + "/bench"

	// Warm up
	resp, err := client.Get(targetURL)
	if err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := client.Get(targetURL)
			if err != nil {
				b.Fatalf("request failed: %v", err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	})
}

func BenchmarkStd_H2_Parallel(b *testing.B) {
	s := testutil.NewH2Server(b, benchHandler())
	defer s.Close()

	client := s.Client()
	targetURL := s.URL() + "/bench"

	// Warm up
	resp, err := client.Get(targetURL)
	if err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := client.Get(targetURL)
			if err != nil {
				b.Fatalf("request failed: %v", err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	})
}
