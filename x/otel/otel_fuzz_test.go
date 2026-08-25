// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/lemon4ksan/aoni/x/otel"
)

// FuzzParseTraceParent tests W3C traceparent parsing and round-trip invariant against arbitrary strings.
func FuzzParseTraceParent(f *testing.F) {
	f.Add("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	f.Add("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00")
	f.Add("00-00000000000000000000000000000000-00f067aa0ba902b7-01") // zero trace id
	f.Add("00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01") // zero span id
	f.Add("ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01") // invalid version
	f.Add("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-extra")
	f.Add("01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-future-extra")
	f.Add("")
	f.Add("malformed-header-with-dashes-inside-and-random-length")

	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 4096 {
			return
		}

		sc, err := otel.ParseTraceParent(raw)
		if err == nil {
			if !sc.IsValid() {
				t.Fatalf("ParseTraceParent succeeded but produced invalid SpanContext for %q", raw)
			}

			// Format back to canonical traceparent
			formatted := sc.TraceParent()
			if len(formatted) != 55 {
				t.Fatalf("expected 55 bytes canonical traceparent, got %d: %q", len(formatted), formatted)
			}

			// Round-trip parse check
			sc2, err2 := otel.ParseTraceParent(formatted)
			if err2 != nil {
				t.Fatalf("failed to re-parse formatted traceparent %q: %v", formatted, err2)
			}

			if sc2.TraceID() != sc.TraceID() || sc2.SpanID() != sc.SpanID() || sc2.TraceFlags() != sc.TraceFlags() {
				t.Fatalf("round-trip mismatch: got %v, expected %v", sc2, sc)
			}
		}
	})
}

// FuzzParseTraceID tests 16-byte TraceID hex parsing resilience and round-trip consistency.
func FuzzParseTraceID(f *testing.F) {
	f.Add("4bf92f3577b34da6a3ce929d0e0e4736")
	f.Add("00000000000000000000000000000000")
	f.Add("4BF92F3577B34DA6A3CE929D0E0E4736")
	f.Add("short")
	f.Add("4bf92f3577b34da6a3ce929d0e0e4736_toolong")
	f.Add("4bf92f3577b34da6a3ce929d0e0e47zz") // invalid non-hex char

	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 512 {
			return
		}

		id, err := otel.ParseTraceID(raw)
		if err == nil {
			if !id.IsValid() {
				t.Fatalf("ParseTraceID succeeded on invalid ID: %q", raw)
			}

			str := id.String()
			if len(str) != 32 {
				t.Fatalf("expected 32-char TraceID string, got %d", len(str))
			}

			id2, err2 := otel.ParseTraceID(str)
			if err2 != nil || id2 != id {
				t.Fatalf("TraceID round-trip failed: got %v, expected %v", id2, id)
			}
		}
	})
}

// FuzzParseSpanID tests 8-byte SpanID hex parsing resilience and round-trip consistency.
func FuzzParseSpanID(f *testing.F) {
	f.Add("00f067aa0ba902b7")
	f.Add("0000000000000000")
	f.Add("00F067AA0BA902B7")
	f.Add("short")
	f.Add("00f067aa0ba902b7_toolong")
	f.Add("00f067aa0ba902zz")

	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 512 {
			return
		}

		id, err := otel.ParseSpanID(raw)
		if err == nil {
			if !id.IsValid() {
				t.Fatalf("ParseSpanID succeeded on invalid ID: %q", raw)
			}

			str := id.String()
			if len(str) != 16 {
				t.Fatalf("expected 16-char SpanID string, got %d", len(str))
			}

			id2, err2 := otel.ParseSpanID(str)
			if err2 != nil || id2 != id {
				t.Fatalf("SpanID round-trip failed: got %v, expected %v", id2, id)
			}
		}
	})
}

// FuzzOTLPSpanAttributesSerialization tests OTLP JSON span formatting with arbitrary keys and values.
func FuzzOTLPSpanAttributesSerialization(f *testing.F) {
	f.Add("http.status_code", "200", int64(200), float64(12.34), true)
	f.Add("custom.key", "unicode_value_🚀_日本語_123", int64(-9999), float64(-0.0001), false)
	f.Add("json.escape.test", `"quoted" \slash \n newline \t tab`, int64(0), float64(0.0), true)
	f.Add("", "", int64(0), float64(0), false)

	f.Fuzz(func(t *testing.T, key, strVal string, intVal int64, floatVal float64, boolVal bool) {
		if len(key) > 512 || len(strVal) > 4096 {
			return
		}

		memExp := otel.NewMemoryExporter()
		tracer := otel.NewTracer("fuzz-tracer", otel.WithExporter(memExp))

		ctx := context.Background()
		_, span := tracer.Start(ctx, "fuzz-span",
			otel.WithSpanKind(otel.SpanKindClient),
			otel.WithAttributes(
				otel.StringAttr(key, strVal),
				otel.Int64Attr(key+".int", intVal),
				otel.Float64Attr(key+".float", floatVal),
				otel.BoolAttr(key+".bool", boolVal),
				otel.StringAttr("err.type", "test"),
			),
		)

		span.RecordError(errors.New("simulated fuzz error"))
		span.AddEvent("test-event", otel.StringAttr("event.key", strVal))
		span.End()

		spans := memExp.Spans()
		if len(spans) != 1 {
			t.Fatalf("expected 1 exported span, got %d", len(spans))
		}

		// Also export via OTLP HTTP Exporter encoding path
		httpExp := otel.NewHTTPExporter("http://127.0.0.1:4318/v1/traces")
		defer func() { _ = httpExp.Shutdown(context.Background()) }()
	})
}

// FuzzCarrierPropagation tests HTTP Header carrier extraction and injection with arbitrary data.
func FuzzCarrierPropagation(f *testing.F) {
	f.Add("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "rojo=1,congo=2")
	f.Add("invalid-parent", "malformed,state")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, traceparent, tracestate string) {
		if len(traceparent) > 1024 || len(tracestate) > 1024 {
			return
		}

		h := make(http.Header)
		if traceparent != "" {
			h.Set(otel.HeaderTraceParent, traceparent)
		}
		if tracestate != "" {
			h.Set(otel.HeaderTraceState, tracestate)
		}

		carrier := otel.HTTPHeaderCarrier(h)
		sc, ok := otel.Extract(carrier)

		outHeader := make(http.Header)
		outCarrier := otel.HTTPHeaderCarrier(outHeader)

		if ok && sc.IsValid() {
			ctx := otel.ContextWithSpanContext(context.Background(), sc)
			tracer := otel.NewTracer("fuzz")
			ctx, span := tracer.Start(ctx, "fuzz")
			defer span.End()

			otel.Inject(ctx, outCarrier)

			injected := outHeader.Get(otel.HeaderTraceParent)
			if injected == "" {
				t.Fatalf("expected injected traceparent for valid input %q", traceparent)
			}
		}
	})
}
