// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel

import (
	"context"
	"net/http"

	"github.com/lemon4ksan/aoni"
)

// TextMapCarrier provides key-value header access for W3C distributed trace propagation.
type TextMapCarrier interface {
	// Get returns the value associated with the passed key.
	Get(key string) string
	// Set stores the key-value pair.
	Set(key, value string)
	// Keys lists the keys stored in this carrier.
	Keys() []string
}

// RequestCarrier is a zero-allocation [TextMapCarrier] adapter wrapping an [aoni.Request].
type RequestCarrier struct {
	req aoni.Request
}

// NewRequestCarrier wraps an [aoni.Request] into a high-performance [TextMapCarrier].
func NewRequestCarrier(req aoni.Request) RequestCarrier {
	return RequestCarrier{req: req}
}

// Get retrieves the header value for key from the underlying [aoni.Request].
func (c RequestCarrier) Get(key string) string {
	if c.req == nil {
		return ""
	}
	return c.req.Header(key)
}

// Set stores the key-value pair into the underlying [aoni.Request] headers.
func (c RequestCarrier) Set(key, value string) {
	if c.req != nil {
		c.req.SetHeader(key, value)
	}
}

// Keys extracts all unique header names from the request.
func (c RequestCarrier) Keys() []string {
	if c.req == nil {
		return nil
	}

	keys := make([]string, 0, 16)
	for k := range c.req.Headers() {
		keys = append(keys, string(k))
	}
	return keys
}

// HTTPHeaderCarrier is a [TextMapCarrier] adapter wrapping a standard [http.Header] map.
type HTTPHeaderCarrier http.Header

// Get retrieves the first value associated with key.
func (c HTTPHeaderCarrier) Get(key string) string {
	return http.Header(c).Get(key)
}

// Set stores key-value pair into the header map.
func (c HTTPHeaderCarrier) Set(key, value string) {
	http.Header(c).Set(key, value)
}

// Keys lists all header keys.
func (c HTTPHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// Inject serializes the W3C traceparent and tracestate from ctx into carrier.
func Inject(ctx context.Context, carrier TextMapCarrier) {
	if carrier == nil || ctx == nil {
		return
	}

	span := SpanFromContext(ctx)
	if span == nil {
		return
	}

	sc := span.SpanContext()
	if !sc.IsValid() {
		return
	}

	carrier.Set(HeaderTraceParent, sc.TraceParent())
	if state := sc.TraceState(); state != "" {
		carrier.Set(HeaderTraceState, state)
	}
}

// Extract deserializes W3C traceparent and tracestate from carrier into a [SpanContext].
func Extract(carrier TextMapCarrier) (SpanContext, bool) {
	if carrier == nil {
		return SpanContext{}, false
	}

	parentHeader := carrier.Get(HeaderTraceParent)
	if parentHeader == "" {
		return SpanContext{}, false
	}

	sc, err := ParseTraceParent(parentHeader)
	if err != nil {
		return SpanContext{}, false
	}

	if stateHeader := carrier.Get(HeaderTraceState); stateHeader != "" {
		sc.traceState = stateHeader
	}

	return sc, true
}
