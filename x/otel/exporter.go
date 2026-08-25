// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/generic"
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
type OTLPOption = generic.Option[*OTLPHTTPExporter]

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

// NewExporter creates an OTLP/HTTP exporter targeting endpoint (alias for [NewOTLPHTTPExporter]).
func NewExporter(endpoint string, opts ...OTLPOption) *OTLPHTTPExporter {
	return NewOTLPHTTPExporter(endpoint, opts...)
}

// NewHTTPExporter creates an OTLP/HTTP exporter targeting endpoint (alias for [NewOTLPHTTPExporter]).
func NewHTTPExporter(endpoint string, opts ...OTLPOption) *OTLPHTTPExporter {
	return NewOTLPHTTPExporter(endpoint, opts...)
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

	generic.ApplyOptions(e, opts...)

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

var jsonBufferPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, 4096))
	},
}

func buildOTLPJSON(batch []*SpanSnapshot) []byte {
	buf := jsonBufferPool.Get().(*bytes.Buffer)
	buf.Reset()

	buf.WriteString(`{"resourceSpans":[{"scopeSpans":[{"scope":{"name":"github.com/lemon4ksan/aoni/x/otel","version":"1.0.0"},"spans":[`)

	for i, s := range batch {
		if s == nil {
			continue
		}
		if i > 0 {
			buf.WriteByte(',')
		}

		buf.WriteString(`{"traceId":"`)
		buf.WriteString(s.SpanContext.TraceID().String())
		buf.WriteString(`","spanId":"`)
		buf.WriteString(s.SpanContext.SpanID().String())
		buf.WriteString(`"`)

		if s.ParentSpanContext.IsValid() {
			buf.WriteString(`,"parentSpanId":"`)
			buf.WriteString(s.ParentSpanContext.SpanID().String())
			buf.WriteString(`"`)
		}

		buf.WriteString(`,"name":`)
		writeJSONString(buf, s.Name)
		buf.WriteString(`,"kind":`)
		buf.WriteString(strconv.Itoa(int(s.Kind)))
		buf.WriteString(`,"startTimeUnixNano":"`)
		buf.WriteString(strconv.FormatInt(s.StartTime.UnixNano(), 10))
		buf.WriteString(`","endTimeUnixNano":"`)
		buf.WriteString(strconv.FormatInt(s.EndTime.UnixNano(), 10))
		buf.WriteString(`"`)

		// Status
		buf.WriteString(`,"status":{"code":`)
		buf.WriteString(strconv.Itoa(int(s.Status)))
		if s.StatusDescription != "" {
			buf.WriteString(`,"message":`)
			writeJSONString(buf, s.StatusDescription)
		}
		buf.WriteString(`}`)

		// Attributes
		if len(s.Attributes) > 0 {
			buf.WriteString(`,"attributes":[`)
			for j, a := range s.Attributes {
				if j > 0 {
					buf.WriteByte(',')
				}
				writeOTLPAttribute(buf, a.Key, a.Value)
			}
			buf.WriteString(`]`)
		}

		// Events
		if len(s.Events) > 0 {
			buf.WriteString(`,"events":[`)
			for j, ev := range s.Events {
				if j > 0 {
					buf.WriteByte(',')
				}
				buf.WriteString(`{"timeUnixNano":"`)
				buf.WriteString(strconv.FormatInt(ev.Timestamp.UnixNano(), 10))
				buf.WriteString(`","name":`)
				writeJSONString(buf, ev.Name)
				if len(ev.Attributes) > 0 {
					buf.WriteString(`,"attributes":[`)
					for k, ea := range ev.Attributes {
						if k > 0 {
							buf.WriteByte(',')
						}
						writeOTLPAttribute(buf, ea.Key, ea.Value)
					}
					buf.WriteString(`]`)
				}
				buf.WriteString(`}`)
			}
			buf.WriteString(`]`)
		}

		buf.WriteByte('}')
	}

	buf.WriteString(`]}]}]}`)

	res := make([]byte, buf.Len())
	copy(res, buf.Bytes())
	jsonBufferPool.Put(buf)
	return res
}

func writeJSONString(buf *bytes.Buffer, s string) {
	b, _ := json.Marshal(s)
	buf.Write(b)
}

func writeOTLPAttribute(buf *bytes.Buffer, key string, val any) {
	buf.WriteString(`{"key":`)
	writeJSONString(buf, key)
	buf.WriteString(`,"value":{`)
	switch v := val.(type) {
	case string:
		buf.WriteString(`"stringValue":`)
		writeJSONString(buf, v)
	case int:
		buf.WriteString(`"intValue":"`)
		buf.WriteString(strconv.Itoa(v))
		buf.WriteString(`"`)
	case int64:
		buf.WriteString(`"intValue":"`)
		buf.WriteString(strconv.FormatInt(v, 10))
		buf.WriteString(`"`)
	case float64:
		buf.WriteString(`"doubleValue":`)
		buf.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
	case bool:
		if v {
			buf.WriteString(`"boolValue":true`)
		} else {
			buf.WriteString(`"boolValue":false`)
		}
	case fmt.Stringer:
		buf.WriteString(`"stringValue":`)
		writeJSONString(buf, v.String())
	default:
		buf.WriteString(`"stringValue":`)
		writeJSONString(buf, fmt.Sprintf("%v", v))
	}
	buf.WriteString(`}}`)
}
