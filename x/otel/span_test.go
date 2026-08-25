// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel

import (
	"context"
	"errors"
	"testing"
)

func TestSpan_LifecycleAndEvents(t *testing.T) {
	memExp := NewMemoryExporter()
	tracer := NewTracer("test-tracer", WithExporter(memExp))

	ctx := context.Background()
	ctx, span := tracer.Start(ctx, "ParentOperation", WithSpanKind(SpanKindInternal))

	span.SetAttribute("custom.key", "value123")
	span.AddEvent("cache_lookup", StringAttr("result", "hit"))

	// Start child span
	_, childSpan := tracer.Start(ctx, "ChildOperation", WithSpanKind(SpanKindClient))
	childSpan.AddEvent("dns_start")
	childSpan.AddEvent("dns_done")
	childSpan.RecordError(errors.New("timeout"))
	childSpan.End()

	span.SetStatus(StatusOk, "completed")
	span.End()

	spans := memExp.Spans()
	if len(spans) != 2 {
		t.Fatalf("expected 2 exported spans, got %d", len(spans))
	}

	// Child span should be first exported
	child := spans[0]
	if child.Name != "ChildOperation" {
		t.Errorf("expected child span name 'ChildOperation', got %s", child.Name)
	}
	if child.Status != StatusError {
		t.Errorf("expected child status ERROR, got %v", child.Status)
	}
	if len(child.Events) != 3 { // dns_start, dns_done, exception
		t.Errorf("expected 3 events on child, got %d", len(child.Events))
	}

	// Parent span
	parent := spans[1]
	if parent.Name != "ParentOperation" {
		t.Errorf("expected parent span name 'ParentOperation', got %s", parent.Name)
	}
	if parent.Status != StatusOk {
		t.Errorf("expected parent status OK, got %v", parent.Status)
	}

	// Verify Parent-Child Relationship (TraceID must match, ParentSpanID must match)
	if child.SpanContext.TraceID() != parent.SpanContext.TraceID() {
		t.Errorf("expected identical TraceID for parent and child (%s vs %s)",
			parent.SpanContext.TraceID().String(), child.SpanContext.TraceID().String())
	}
	if child.ParentSpanContext.SpanID() != parent.SpanContext.SpanID() {
		t.Errorf("expected child's ParentSpanID to match parent's SpanID (%s vs %s)",
			parent.SpanContext.SpanID().String(), child.ParentSpanContext.SpanID().String())
	}
}

func TestTracer_Sampling(t *testing.T) {
	memExp := NewMemoryExporter()

	// 1. Never Sample
	tracerDrop := NewTracer("drop-service", WithExporter(memExp), WithSampler(NeverSampleSampler{}))
	_, span := tracerDrop.Start(context.Background(), "DroppedSpan")
	span.End()

	if len(memExp.Spans()) != 0 {
		t.Fatalf("expected 0 spans with NeverSampleSampler, got %d", len(memExp.Spans()))
	}

	// 2. Ratio Sampler
	ratioSampler := NewRatioSampler(0.5)
	var sampledCount int
	trials := 1000

	for i := 0; i < trials; i++ {
		tID := NewTraceID()
		if ratioSampler.ShouldSample(SpanContext{}, tID, "test", SpanKindInternal) {
			sampledCount++
		}
	}

	// 50% should be around 500 (+/- 100)
	if sampledCount < 400 || sampledCount > 600 {
		t.Errorf("ratio sampler out of expected range for 0.5: got %d / %d", sampledCount, trials)
	}
}

func TestSpan_ContextExtraction(t *testing.T) {
	tracer := NewTracer("test")
	ctx, span := tracer.Start(context.Background(), "Root")
	defer span.End()

	activeSpan := SpanFromContext(ctx)
	if activeSpan == nil {
		t.Fatal("expected active span in context")
	}

	traceID := TraceIDFromContext(ctx)
	if traceID != span.SpanContext().TraceID().String() {
		t.Errorf("expected traceID %s, got %s", span.SpanContext().TraceID().String(), traceID)
	}
}
