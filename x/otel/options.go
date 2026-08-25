// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel

import (
	"fmt"
	"net/url"

	"github.com/lemon4ksan/aoni"
)

// Config holds settings for the OpenTelemetry client middleware.
type Config struct {
	Tracer            *Tracer
	Filter            func(req aoni.Request) bool
	SpanNameFormatter func(req aoni.Request) string
	CustomAttributes  func(req aoni.Request) []Attribute
	TraceEvents       bool
	PropagateContext  bool
}

// Option applies configuration to [Config].
type Option func(*Config)

// DefaultConfig returns the default middleware configuration.
func DefaultConfig() Config {
	return Config{
		PropagateContext: true,
		TraceEvents:      true,
		SpanNameFormatter: func(req aoni.Request) string {
			if req == nil {
				return "HTTP"
			}
			method := req.Method()
			if method == "" {
				method = "GET"
			}
			rawURL := req.URL()
			if u, err := url.Parse(rawURL); err == nil && u != nil {
				if u.Path != "" {
					return fmt.Sprintf("%s %s", method, u.Path)
				}
			}
			return method
		},
	}
}

// WithTracer assigns the [Tracer] instance used by the middleware.
func WithTracer(tracer *Tracer) Option {
	return func(c *Config) {
		c.Tracer = tracer
	}
}

// WithFilter sets a request filter callback. If the filter returns false,
// tracing and span generation are bypassed for that request.
func WithFilter(filter func(req aoni.Request) bool) Option {
	return func(c *Config) {
		c.Filter = filter
	}
}

// WithSpanNameFormatter customizes how operation span names are generated.
func WithSpanNameFormatter(formatter func(req aoni.Request) string) Option {
	return func(c *Config) {
		if formatter != nil {
			c.SpanNameFormatter = formatter
		}
	}
}

// WithCustomAttributes registers a callback to attach dynamic attributes on each outgoing request span.
func WithCustomAttributes(fn func(req aoni.Request) []Attribute) Option {
	return func(c *Config) {
		c.CustomAttributes = fn
	}
}

// WithTraceEvents enables or disables recording detailed connection events (DNS, TLS, Connect).
func WithTraceEvents(enable bool) Option {
	return func(c *Config) {
		c.TraceEvents = enable
	}
}

// WithPropagateContext controls whether W3C traceparent headers are injected into outgoing requests.
func WithPropagateContext(propagate bool) Option {
	return func(c *Config) {
		c.PropagateContext = propagate
	}
}

// WithServiceName configures the logical service name on the default tracer.
func WithServiceName(name string) Option {
	return func(c *Config) {
		if c.Tracer == nil {
			c.Tracer = NewTracer(name, WithTracerServiceName(name))
		} else {
			c.Tracer.serviceName = name
		}
	}
}
