// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package otel provides a zero-dependency, ultra-high-performance OpenTelemetry (OTel)
// distributed tracing and observability engine for the aoni networking stack.
//
// In accordance with the aoni core manifesto, this package has 0 third-party external dependencies,
// guarantees zero heap allocations along the hot execution path, and strictly conforms to:
//   - W3C TraceContext Recommendation (traceparent and tracestate propagation)
//   - OpenTelemetry HTTP Client Semantic Conventions (semconv v1.26+)
//   - OTLP/HTTP JSON wire protocol for direct export to Grafana Tempo, Jaeger, and OTel Collector.
//
// # Basic Usage with aoni.Client:
//
//	tracer := otel.NewTracer("my-service",
//		otel.WithOTLPExporter("http://localhost:4318"),
//	)
//	defer tracer.Shutdown(context.Background())
//
//	client := aoni.NewClient(nil,
//		option.WithMiddleware(otel.NewMiddleware(
//			otel.WithTracer(tracer),
//			otel.WithTraceEvents(true),
//		)),
//	)
//
//	// Outgoing requests will automatically generate client spans, inject W3C traceparent,
//	// record DNS/TLS/Connect phases, and stream trace data to the OTel collector.
//	user, err := client.Get[User](ctx, "https://api.example.com/users/42")
package otel
