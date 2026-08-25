// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel

import (
	"context"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/generic"
)

// Sampler decides whether a new trace span should be sampled and exported.
type Sampler interface {
	ShouldSample(parentSc SpanContext, traceID TraceID, name string, kind SpanKind) bool
}

// AlwaysSampleSampler samples 100% of all spans.
type AlwaysSampleSampler struct{}

func (AlwaysSampleSampler) ShouldSample(_ SpanContext, _ TraceID, _ string, _ SpanKind) bool {
	return true
}

// NeverSampleSampler drops all spans.
type NeverSampleSampler struct{}

func (NeverSampleSampler) ShouldSample(_ SpanContext, _ TraceID, _ string, _ SpanKind) bool {
	return false
}

// RatioSampler samples a deterministic ratio of traces (e.g. 0.1 = 10%).
type RatioSampler struct {
	threshold uint64
}

// NewRatioSampler constructs a probabilistic [RatioSampler] with ratio in range [0.0, 1.0].
func NewRatioSampler(ratio float64) RatioSampler {
	if ratio >= 1.0 {
		return RatioSampler{threshold: ^uint64(0)}
	}
	if ratio <= 0.0 {
		return RatioSampler{threshold: 0}
	}
	return RatioSampler{threshold: uint64(ratio * float64(^uint64(0)))}
}

func (s RatioSampler) ShouldSample(parentSc SpanContext, traceID TraceID, _ string, _ SpanKind) bool {
	if parentSc.IsValid() {
		return parentSc.IsSampled()
	}
	// Hash lowest 8 bytes of traceID
	val := uint64(traceID[8])<<56 | uint64(traceID[9])<<48 | uint64(traceID[10])<<40 | uint64(traceID[11])<<32 |
		uint64(traceID[12])<<24 | uint64(traceID[13])<<16 | uint64(traceID[14])<<8 | uint64(traceID[15])
	return val <= s.threshold
}

// StartConfig holds configuration for starting a new span.
type StartConfig struct {
	Kind       SpanKind
	Attributes []Attribute
	StartTime  time.Time
}

// StartOption applies configuration to [StartConfig].
type StartOption = generic.Option[*StartConfig]

// WithSpanKind configures the [SpanKind] for the span.
func WithSpanKind(kind SpanKind) StartOption {
	return func(c *StartConfig) {
		c.Kind = kind
	}
}

// WithAttributes adds initial attributes when starting the span.
func WithAttributes(attrs ...Attribute) StartOption {
	return func(c *StartConfig) {
		c.Attributes = append(c.Attributes, attrs...)
	}
}

// WithStartTime overrides the start time of the span.
func WithStartTime(t time.Time) StartOption {
	return func(c *StartConfig) {
		c.StartTime = t
	}
}

// Tracer creates and manages Spans in a distributed trace.
type Tracer struct {
	name        string
	serviceName string
	sampler     Sampler
	exporter    Exporter
	mu          sync.RWMutex
	closed      bool
}

// TracerOption configures a [Tracer].
type TracerOption = generic.Option[*Tracer]

// WithTracerServiceName sets the logical service name reported in Resource telemetry.
func WithTracerServiceName(name string) TracerOption {
	return func(t *Tracer) {
		t.serviceName = name
	}
}

// WithSampler assigns the sampling decision policy.
func WithSampler(sampler Sampler) TracerOption {
	return func(t *Tracer) {
		if sampler != nil {
			t.sampler = sampler
		}
	}
}

// WithExporter sets the trace span exporter.
func WithExporter(exporter Exporter) TracerOption {
	return func(t *Tracer) {
		t.exporter = exporter
	}
}

// NewTracer constructs a new thread-safe [Tracer].
func NewTracer(name string, opts ...TracerOption) *Tracer {
	t := &Tracer{
		name:        name,
		serviceName: name,
		sampler:     AlwaysSampleSampler{},
	}
	generic.ApplyOptions(t, opts...)
	return t
}

// ServiceName returns the configured service name.
func (t *Tracer) ServiceName() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.serviceName
}

// Start initiates a new [Span] as a child of the span active in ctx (if any),
// returning the updated context and the new span.
func (t *Tracer) Start(ctx context.Context, spanName string, opts ...StartOption) (context.Context, *Span) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := StartConfig{
		Kind:      SpanKindInternal,
		StartTime: time.Now(),
	}
	generic.ApplyOptions(&cfg, opts...)

	var parentSc SpanContext
	if parentSpan := SpanFromContext(ctx); parentSpan != nil {
		parentSc = parentSpan.SpanContext()
	} else {
		parentSc = RemoteSpanContextFromContext(ctx)
	}

	var traceID TraceID
	if parentSc.IsValid() {
		traceID = parentSc.TraceID()
	} else {
		traceID = NewTraceID()
	}

	spanID := NewSpanID()

	var flags TraceFlags
	if t.sampler.ShouldSample(parentSc, traceID, spanName, cfg.Kind) {
		flags |= FlagSampled
	}

	sc := NewSpanContext(traceID, spanID, flags, parentSc.TraceState(), false)

	span := acquireSpan(t, spanName, sc, parentSc, cfg.Kind, cfg.StartTime)
	if len(cfg.Attributes) > 0 {
		span.SetAttributes(cfg.Attributes...)
	}

	return ContextWithSpan(ctx, span), span
}

// processSpan handles completed spans by passing them to the configured exporter.
func (t *Tracer) processSpan(s *Span) {
	t.mu.RLock()
	exporter := t.exporter
	closed := t.closed
	t.mu.RUnlock()

	if closed || exporter == nil || !s.SpanContext().IsSampled() {
		releaseSpan(s)
		return
	}

	// Export span (Exporter handles memory lifecycle or releases span)
	_ = exporter.Export(context.Background(), []*Span{s})
}

// Shutdown flushes pending spans and stops any background export workers.
func (t *Tracer) Shutdown(ctx context.Context) error {
	t.mu.Lock()
	t.closed = true
	exporter := t.exporter
	t.mu.Unlock()

	if exporter != nil {
		return exporter.Shutdown(ctx)
	}
	return nil
}
