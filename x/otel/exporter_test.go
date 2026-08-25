// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestOTLPHTTPExporter_Export(t *testing.T) {
	var receivedBatches atomic.Int32
	var receivedSpans atomic.Int32

	// Mock OTLP/HTTP collector receiver
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type")
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
		}

		var payload struct {
			ResourceSpans []struct {
				ScopeSpans []struct {
					Spans []any `json:"spans"`
				} `json:"scopeSpans"`
			} `json:"resourceSpans"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("invalid OTLP JSON payload received: %v", err)
		}

		receivedBatches.Add(1)
		for _, rs := range payload.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				receivedSpans.Add(int32(len(ss.Spans)))
			}
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exporter := NewOTLPHTTPExporter(server.URL, WithBatchSize(10))
	tracer := NewTracer("otlp-service", WithExporter(exporter))

	// Generate 25 spans
	for i := range 25 {
		_, span := tracer.Start(context.Background(), "OTLPOperation", WithSpanKind(SpanKindClient))
		span.SetAttribute("test.index", i)
		span.AddEvent("batch_step")
		span.End()
	}

	// Flush and shutdown exporter
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := exporter.Shutdown(ctx); err != nil {
		t.Fatalf("error shutting down exporter: %v", err)
	}

	if receivedSpans.Load() != 25 {
		t.Errorf("expected 25 spans received by collector, got %d", receivedSpans.Load())
	}
	if receivedBatches.Load() == 0 {
		t.Errorf("expected at least 1 HTTP batch sent to collector")
	}
}

func BenchmarkSpanCreation(b *testing.B) {
	tracer := NewTracer("bench-tracer", WithSampler(NeverSampleSampler{}))
	ctx := context.Background()

	b.ReportAllocs()

	for b.Loop() {
		_, span := tracer.Start(ctx, "FastOp")
		span.End()
	}
}
