// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel

import (
	"context"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/foundation/generic"
)

// NewMiddleware constructs an [aoni.Middleware] that automatically creates OpenTelemetry client Spans,
// populates standard HTTP Semantic Conventions, injects W3C traceparent headers, and records execution status.
func NewMiddleware(opts ...Option) aoni.Middleware {
	cfg := DefaultConfig()
	generic.ApplyOptions(&cfg, opts...)

	if cfg.Tracer == nil {
		cfg.Tracer = NewTracer("aoni-client")
	}

	return func(next aoni.RequestDoer) aoni.RequestDoer {
		return aoni.DoerFunc(func(req aoni.Request) (aoni.Response, error) {
			// If filter is declared and rejects request, bypass tracing
			if cfg.Filter != nil && !cfg.Filter(req) {
				return next.Do(req)
			}

			reqCtx := req.Context()
			if reqCtx == nil {
				reqCtx = context.Background()
			}

			spanName := "HTTP"
			if cfg.SpanNameFormatter != nil {
				spanName = cfg.SpanNameFormatter(req)
			}

			// Start Client Span
			startAttrs := HTTPClientRequestAttributes(req)
			if IsGRPCRequest(req) {
				startAttrs = append(startAttrs, GRPCClientRequestAttributes(req)...)
			}

			ctx, span := cfg.Tracer.Start(reqCtx, spanName,
				WithSpanKind(SpanKindClient),
				WithStartTime(time.Now()),
				WithAttributes(startAttrs...),
			)
			defer span.End()

			// Attach custom dynamic attributes if provided
			if cfg.CustomAttributes != nil {
				if customAttrs := cfg.CustomAttributes(req); len(customAttrs) > 0 {
					span.SetAttributes(customAttrs...)
				}
			}

			// Record request body size if present
			if body := req.BodyBytes(); len(body) > 0 {
				span.SetAttribute(KeyHTTPRequestBodySize, len(body))
			}

			// Update request context
			req.SetContext(ctx)

			// Inject W3C TraceContext into outgoing headers
			if cfg.PropagateContext {
				Inject(ctx, NewRequestCarrier(req))
			}

			// Execute downstream request
			resp, err := next.Do(req)

			// Record execution outcome
			if err != nil {
				span.RecordError(err)
				span.SetAttribute(KeyErrorType, "NetworkError")
				span.SetStatus(StatusError, err.Error())
				return resp, err
			}

			if resp != nil {
				span.SetAttributes(HTTPClientResponseAttributes(resp)...)
				if IsGRPCRequest(req) {
					span.SetAttributes(GRPCClientResponseAttributes(resp)...)
				}

				if respBody := resp.BodyBytes(); len(respBody) > 0 {
					span.SetAttribute(KeyHTTPResponseBodySize, len(respBody))
				}

				statusCode := resp.StatusCode()
				grpcStatus := resp.Header("grpc-status")
				if grpcStatus != "" && grpcStatus != "0" {
					span.SetStatus(StatusError, "gRPC status: "+grpcStatus)
				} else if statusCode >= 400 {
					span.SetStatus(StatusError, resp.Status())
				} else {
					span.SetStatus(StatusOk, "")
				}
			}

			return resp, nil
		})
	}
}
