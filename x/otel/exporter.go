// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Exporter receives completed Spans and dispatches them to a telemetry backend.
type Exporter interface {
	// Export sends a batch of Spans to the destination.
	Export(ctx context.Context, spans []*Span) error
	// Shutdown flushes buffered spans and releases resources.
	Shutdown(ctx context.Context) error
}

// MemoryExporter buffers spans in memory for unit testing, assertions, and local debugging.
type MemoryExporter struct {
	mu    sync.RWMutex
	spans []*SpanSnapshot
}

// SpanSnapshot is an immutable record of an exported Span.
type SpanSnapshot struct {
	Name              string
	SpanContext       SpanContext
	ParentSpanContext SpanContext
	Kind              SpanKind
	StartTime         time.Time
	EndTime           time.Time
	Duration          time.Duration
	Status            StatusCode
	StatusDescription string
	Attributes        []Attribute
	Events            []Event
}

// NewMemoryExporter constructs an in-memory test [MemoryExporter].
func NewMemoryExporter() *MemoryExporter {
	return &MemoryExporter{
		spans: make([]*SpanSnapshot, 0, 32),
	}
}

// Export records a snapshot of each Span in the batch and recycles the span.
func (m *MemoryExporter) Export(_ context.Context, spans []*Span) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range spans {
		if s == nil {
			continue
		}
		snap := &SpanSnapshot{
			Name:              s.Name(),
			SpanContext:       s.SpanContext(),
			ParentSpanContext: s.ParentSpanContext(),
			Kind:              s.Kind(),
			StartTime:         s.StartTime(),
			EndTime:           s.EndTime(),
			Duration:          s.Duration(),
			Attributes:        s.Attributes(),
			Events:            s.Events(),
		}
		snap.Status, snap.StatusDescription = s.Status()
		m.spans = append(m.spans, snap)
		releaseSpan(s)
	}
	return nil
}

// Spans returns a copy of all captured Span snapshots.
func (m *MemoryExporter) Spans() []*SpanSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]*SpanSnapshot, len(m.spans))
	copy(res, m.spans)
	return res
}

// Reset clears all buffered snapshots.
func (m *MemoryExporter) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spans = m.spans[:0]
}

// Shutdown implements [Exporter.Shutdown].
func (m *MemoryExporter) Shutdown(_ context.Context) error {
	m.Reset()
	return nil
}

// OTLPHTTPExporter exports spans via standard OTLP/HTTP JSON wire protocol (POST /v1/traces)
// to any OpenTelemetry Collector, Grafana Tempo, or Jaeger without external SDK dependencies.
type OTLPHTTPExporter struct {
	endpoint   string
	httpClient *http.Client
	queue      chan *SpanSnapshot
	done       chan struct{}
	wg         sync.WaitGroup
	batchSize  int
	timeout    time.Duration
}

// OTLPOption configures an [OTLPHTTPExporter].
type OTLPOption func(*OTLPHTTPExporter)

// WithBatchSize sets maximum spans per HTTP export request.
func WithBatchSize(size int) OTLPOption {
	return func(e *OTLPHTTPExporter) {
		if size > 0 {
			e.batchSize = size
		}
	}
}

// WithHTTPClient overrides the internal [*http.Client] used for sending OTLP batches.
func WithHTTPClient(client *http.Client) OTLPOption {
	return func(e *OTLPHTTPExporter) {
		if client != nil {
			e.httpClient = client
		}
	}
}

// NewOTLPHTTPExporter creates an exporter targeting an OTLP/HTTP endpoint (e.g. "http://localhost:4318").
func NewOTLPHTTPExporter(endpoint string, opts ...OTLPOption) *OTLPHTTPExporter {
	if endpoint == "" {
		endpoint = "http://localhost:4318"
	}
	// Normalize endpoint to /v1/traces
	if len(endpoint) > 0 && endpoint[len(endpoint)-1] == '/' {
		endpoint = endpoint[:len(endpoint)-1]
	}
	if len(endpoint) < 10 || endpoint[len(endpoint)-10:] != "/v1/traces" {
		endpoint += "/v1/traces"
	}

	e := &OTLPHTTPExporter{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		queue:     make(chan *SpanSnapshot, 2048),
		done:      make(chan struct{}),
		batchSize: 128,
		timeout:   5 * time.Second,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}

	e.wg.Add(1)
	go e.worker()

	return e
}

// Export enqueues spans for asynchronous batch dispatch and recycles spans.
func (e *OTLPHTTPExporter) Export(_ context.Context, spans []*Span) error {
	for _, s := range spans {
		if s == nil {
			continue
		}
		snap := &SpanSnapshot{
			Name:              s.Name(),
			SpanContext:       s.SpanContext(),
			ParentSpanContext: s.ParentSpanContext(),
			Kind:              s.Kind(),
			StartTime:         s.StartTime(),
			EndTime:           s.EndTime(),
			Duration:          s.Duration(),
			Attributes:        s.Attributes(),
			Events:            s.Events(),
		}
		snap.Status, snap.StatusDescription = s.Status()
		releaseSpan(s)

		select {
		case e.queue <- snap:
		default:
			// Drop span if queue is completely saturated to preserve zero-overhead execution
		}
	}
	return nil
}

// worker periodically flushes queued spans to the OTLP/HTTP endpoint.
func (e *OTLPHTTPExporter) worker() {
	defer e.wg.Done()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	batch := make([]*SpanSnapshot, 0, e.batchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		e.sendBatch(batch)
		batch = batch[:0]
	}

	for {
		select {
		case <-e.done:
			// Drain remaining items from queue
			for {
				select {
				case snap := <-e.queue:
					batch = append(batch, snap)
					if len(batch) >= e.batchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case snap := <-e.queue:
			batch = append(batch, snap)
			if len(batch) >= e.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// sendBatch encodes batch to OTLP JSON and sends via HTTP POST.
func (e *OTLPHTTPExporter) sendBatch(batch []*SpanSnapshot) {
	if len(batch) == 0 {
		return
	}

	payload := buildOTLPJSON(batch)
	req, err := http.NewRequest(http.MethodPost, e.endpoint, bytes.NewReader(payload))
	if err != nil {
		return
	}

	req.Header.Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := e.httpClient.Do(req)
	if err == nil && resp != nil {
		_ = resp.Body.Close()
	}
}

// Shutdown flushes pending spans and terminates the worker.
func (e *OTLPHTTPExporter) Shutdown(_ context.Context) error {
	select {
	case <-e.done:
		return nil
	default:
		close(e.done)
	}
	e.wg.Wait()
	return nil
}

// OTLP JSON Schema definitions.
type otlpRoot struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

type otlpScopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type otlpSpan struct {
	TraceID           string         `json:"traceId"`
	SpanID            string         `json:"spanId"`
	ParentSpanID      string         `json:"parentSpanId,omitempty"`
	Name              string         `json:"name"`
	Kind              int            `json:"kind"`
	StartTimeUnixNano string         `json:"startTimeUnixNano"`
	EndTimeUnixNano   string         `json:"endTimeUnixNano"`
	Attributes        []otlpKeyValue `json:"attributes,omitempty"`
	Events            []otlpEvent    `json:"events,omitempty"`
	Status            otlpStatus     `json:"status"`
}

type otlpKeyValue struct {
	Key   string          `json:"key"`
	Value otlpAnyValueRaw `json:"value"`
}

type otlpAnyValueRaw struct {
	StringValue *string  `json:"stringValue,omitempty"`
	IntValue    *string  `json:"intValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
}

type otlpEvent struct {
	TimeUnixNano string         `json:"timeUnixNano"`
	Name         string         `json:"name"`
	Attributes   []otlpKeyValue `json:"attributes,omitempty"`
}

type otlpStatus struct {
	Message string `json:"message,omitempty"`
	Code    int    `json:"code"`
}

func buildOTLPJSON(batch []*SpanSnapshot) []byte {
	otSpans := make([]otlpSpan, 0, len(batch))

	for _, s := range batch {
		if s == nil {
			continue
		}

		span := otlpSpan{
			TraceID:           s.SpanContext.TraceID().String(),
			SpanID:            s.SpanContext.SpanID().String(),
			Name:              s.Name,
			Kind:              int(s.Kind),
			StartTimeUnixNano: strconv.FormatInt(s.StartTime.UnixNano(), 10),
			EndTimeUnixNano:   strconv.FormatInt(s.EndTime.UnixNano(), 10),
			Status: otlpStatus{
				Code:    int(s.Status),
				Message: s.StatusDescription,
			},
		}

		if s.ParentSpanContext.IsValid() {
			span.ParentSpanID = s.ParentSpanContext.SpanID().String()
		}

		// Attributes
		if len(s.Attributes) > 0 {
			span.Attributes = make([]otlpKeyValue, 0, len(s.Attributes))
			for _, a := range s.Attributes {
				span.Attributes = append(span.Attributes, convertKeyValue(a.Key, a.Value))
			}
		}

		// Events
		if len(s.Events) > 0 {
			span.Events = make([]otlpEvent, 0, len(s.Events))
			for _, ev := range s.Events {
				evAttrs := make([]otlpKeyValue, 0, len(ev.Attributes))
				for _, ea := range ev.Attributes {
					evAttrs = append(evAttrs, convertKeyValue(ea.Key, ea.Value))
				}
				span.Events = append(span.Events, otlpEvent{
					TimeUnixNano: strconv.FormatInt(ev.Timestamp.UnixNano(), 10),
					Name:         ev.Name,
					Attributes:   evAttrs,
				})
			}
		}

		otSpans = append(otSpans, span)
	}

	root := otlpRoot{
		ResourceSpans: []otlpResourceSpans{
			{
				ScopeSpans: []otlpScopeSpans{
					{
						Scope: otlpScope{
							Name:    "github.com/lemon4ksan/aoni/x/otel",
							Version: "1.0.0",
						},
						Spans: otSpans,
					},
				},
			},
		},
	}

	data, _ := json.Marshal(root)
	return data
}

func convertKeyValue(key string, val any) otlpKeyValue {
	kv := otlpKeyValue{Key: key}
	switch v := val.(type) {
	case string:
		kv.Value.StringValue = &v
	case int:
		s := strconv.Itoa(v)
		kv.Value.IntValue = &s
	case int64:
		s := strconv.FormatInt(v, 10)
		kv.Value.IntValue = &s
	case float64:
		kv.Value.DoubleValue = &v
	case bool:
		kv.Value.BoolValue = &v
	case fmt.Stringer:
		s := v.String()
		kv.Value.StringValue = &s
	default:
		s := fmt.Sprintf("%v", v)
		kv.Value.StringValue = &s
	}
	return kv
}

// FastHexEncode encodes src into a lowercase hex string.
func FastHexEncode(src []byte) string {
	return hex.EncodeToString(src)
}
