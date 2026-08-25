// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel

import (
	"context"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/silicon/clock"
	"github.com/lemon4ksan/foundation/silicon/pool"
)

// StatusCode represents the OpenTelemetry canonical span status code.
type StatusCode uint8

const (
	// StatusUnset is the default status code.
	StatusUnset StatusCode = 0
	// StatusOk indicates the operation succeeded.
	StatusOk StatusCode = 1
	// StatusError indicates the operation failed.
	StatusError StatusCode = 2
)

func (s StatusCode) String() string {
	switch s {
	case StatusOk:
		return "OK"
	case StatusError:
		return "ERROR"
	default:
		return "UNSET"
	}
}

// SpanKind describes the relationship between the Span, its parents, and its children.
type SpanKind uint8

const (
	// SpanKindUnspecified is the default span kind.
	SpanKindUnspecified SpanKind = 0
	// SpanKindInternal indicates the span represents an internal operation.
	SpanKindInternal SpanKind = 1
	// SpanKindServer indicates the span represents a server-side request handler.
	SpanKindServer SpanKind = 2
	// SpanKindClient indicates the span represents a client-side synchronous call.
	SpanKindClient SpanKind = 3
	// SpanKindProducer indicates the span represents an asynchronous message producer.
	SpanKindProducer SpanKind = 4
	// SpanKindConsumer indicates the span represents an asynchronous message consumer.
	SpanKindConsumer SpanKind = 5
)

// Event represents a timestamped event on a Span (e.g. DNS resolution or TLS handshake).
type Event struct {
	Name       string
	Timestamp  time.Time
	Attributes []Attribute
}

// Span represents a single active or completed unit of work within a distributed trace.
type Span struct {
	tracer            *Tracer
	name              string
	spanContext       SpanContext
	parentSpanContext SpanContext
	kind              SpanKind
	startTime         time.Time
	endTime           time.Time
	status            StatusCode
	statusDesc        string
	attributes        []Attribute
	events            []Event
	ended             bool
	mu                sync.RWMutex
}

var spanStorage = pool.NewPerPStorage[*Span](func() *Span {
	return &Span{
		attributes: make([]Attribute, 0, 16),
		events:     make([]Event, 0, 8),
	}
})

// acquireSpan retrieves an empty [Span] from the core-pinned PerP storage ring.
func acquireSpan(tracer *Tracer, name string, sc, parentSc SpanContext, kind SpanKind, startTime time.Time) *Span {
	s := spanStorage.Get()
	if s == nil {
		s = &Span{
			attributes: make([]Attribute, 0, 16),
			events:     make([]Event, 0, 8),
		}
	}
	s.tracer = tracer
	s.name = name
	s.spanContext = sc
	s.parentSpanContext = parentSc
	s.kind = kind
	if startTime.IsZero() {
		s.startTime = clock.CoarseTime()
	} else {
		s.startTime = startTime
	}
	s.endTime = time.Time{}
	s.status = StatusUnset
	s.statusDesc = ""
	s.ended = false
	s.attributes = s.attributes[:0]
	s.events = s.events[:0]
	return s
}

// releaseSpan resets and returns s to the core-pinned PerP storage ring.
func releaseSpan(s *Span) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.tracer = nil
	s.name = ""
	s.spanContext = SpanContext{}
	s.parentSpanContext = SpanContext{}
	s.statusDesc = ""
	s.attributes = s.attributes[:0]
	s.events = s.events[:0]
	s.mu.Unlock()
	spanStorage.Put(s)
}

// Name returns the span operation name.
func (s *Span) Name() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

// SetName updates the span operation name.
func (s *Span) SetName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
}

// SpanContext returns the immutable [SpanContext] identifier.
func (s *Span) SpanContext() SpanContext {
	return s.spanContext
}

// ParentSpanContext returns the parent [SpanContext].
func (s *Span) ParentSpanContext() SpanContext {
	return s.parentSpanContext
}

// Kind returns the span's [SpanKind].
func (s *Span) Kind() SpanKind {
	return s.kind
}

// StartTime returns the timestamp when the span began.
func (s *Span) StartTime() time.Time {
	return s.startTime
}

// EndTime returns the timestamp when the span ended.
func (s *Span) EndTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.endTime
}

// Duration returns the total active elapsed time of the span.
func (s *Span) Duration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.endTime.IsZero() {
		return time.Since(s.startTime)
	}
	return s.endTime.Sub(s.startTime)
}

// Status returns the current status code and description.
func (s *Span) Status() (StatusCode, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status, s.statusDesc
}

// SetStatus updates the span status code and description.
func (s *Span) SetStatus(code StatusCode, desc string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	// StatusOk cannot be overridden by StatusError per OTel spec once set, unless error occurs before.
	s.status = code
	s.statusDesc = desc
}

// SetAttribute adds or updates a telemetry attribute on the span.
func (s *Span) SetAttribute(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	for i := range s.attributes {
		if s.attributes[i].Key == key {
			s.attributes[i].Value = value
			return
		}
	}
	s.attributes = append(s.attributes, Attribute{Key: key, Value: value})
}

// SetAttributes appends multiple attributes onto the span.
func (s *Span) SetAttributes(attrs ...Attribute) {
	if len(attrs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.attributes = append(s.attributes, attrs...)
}

// Attributes returns a snapshot of all attributes set on the span.
func (s *Span) Attributes() []Attribute {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]Attribute, len(s.attributes))
	copy(res, s.attributes)
	return res
}

// AddEvent records a timestamped event with optional attributes.
func (s *Span) AddEvent(name string, attrs ...Attribute) {
	s.AddEventWithTime(name, time.Now(), attrs...)
}

// AddEventWithTime records a timestamped event with explicit time and attributes.
func (s *Span) AddEventWithTime(name string, timestamp time.Time, attrs ...Attribute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.events = append(s.events, Event{
		Name:       name,
		Timestamp:  timestamp,
		Attributes: attrs,
	})
}

// Events returns a snapshot of all recorded events on the span.
func (s *Span) Events() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]Event, len(s.events))
	copy(res, s.events)
	return res
}

// RecordError records an error event according to OTel exception semantic conventions
// and sets StatusError on the span.
func (s *Span) RecordError(err error) {
	if err == nil {
		return
	}
	s.AddEvent("exception", ExceptionAttributes(err)...)
	s.SetStatus(StatusError, err.Error())
}

// End marks the span as completed and dispatches it to the tracer's exporter.
func (s *Span) End() {
	s.EndWithTime(time.Now())
}

// EndWithTime marks the span as completed at an explicit timestamp.
func (s *Span) EndWithTime(endTime time.Time) {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	s.endTime = endTime
	tracer := s.tracer
	s.mu.Unlock()

	if tracer != nil {
		tracer.processSpan(s)
	}
}

// Context key for storing active Span.
type contextKey struct{}

var activeSpanKey = contextKey{}

// ContextWithSpan returns a new context carrying the given active [*Span].
func ContextWithSpan(ctx context.Context, span *Span) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, activeSpanKey, span)
}

// SpanFromContext retrieves the active [*Span] from ctx, or nil if none exists.
func SpanFromContext(ctx context.Context) *Span {
	if ctx == nil {
		return nil
	}
	if span, ok := ctx.Value(activeSpanKey).(*Span); ok {
		return span
	}
	return nil
}

// TraceIDFromContext returns the 32-hex TraceID from ctx if an active span exists, or empty string.
func TraceIDFromContext(ctx context.Context) string {
	if span := SpanFromContext(ctx); span != nil {
		return span.SpanContext().TraceID().String()
	}
	return ""
}
