// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/lemon4ksan/foundation/iokit"
	"github.com/lemon4ksan/foundation/net/http/header"

	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/internal/pipeline"
	"github.com/lemon4ksan/aoni/telemetry"
)

// WithCorrelationID constructs an [RequestModifier] setting an end-to-end tracing correlation ID header ("X-Correlation-ID").
func WithCorrelationID(id string) RequestModifier {
	activeID := id
	if activeID == "" {
		activeID = telemetry.GenerateCorrelationID()
	}

	return Custom(func(req Request) {
		cfg := getOrInitRequestConfig(req)
		if cfg.TraceInfo != nil {
			cfg.TraceInfo.CorrelationID = activeID
		}

		req.SetHeader(header.XCorrelationID, activeID)
	})
}

// WithLabel constructs an [RequestModifier] assigning a route or metric label to the request context.
func WithLabel(label string) RequestModifier {
	return Custom(func(req Request) {
		cfg := getOrInitRequestConfig(req)
		cfg.Label = label

		if cfg.TraceInfo != nil {
			cfg.TraceInfo.Label = label
		}
	})
}

// WithDebug constructs an [RequestModifier] marking the request for verbose diagnostic logging.
func WithDebug() RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).Debug = true
	})
}

// WithCurlDump constructs an [RequestModifier] printing an equivalent shell-escaped cURL command to stderr.
func WithCurlDump() RequestModifier {
	return Custom(func(req Request) {
		stdReq := req.HTTPRequest()
		if stdReq != nil {
			dumpStdRequest(stdReq)
			return
		}

		dumpGenericRequest(req)
	})
}

// dumpStdRequest prints a curl-equivalent command for an [http.Request] to stderr.
func dumpStdRequest(stdReq *http.Request) {
	var body []byte
	if stdReq.Body != nil && stdReq.Body != http.NoBody {
		var buf bytes.Buffer

		_, _ = iokit.CopyZeroAlloc(&buf, stdReq.Body)
		body = buf.Bytes()
		stdReq.Body = io.NopCloser(bytes.NewReader(body))
	}

	curl := telemetry.CurlFromRequest(stdReq, body)
	fmt.Fprintf(os.Stderr, "%s\n", curl)
}

// dumpGenericRequest prints a curl-equivalent command for an [Request] to stderr.
func dumpGenericRequest(req Request) {
	body := req.BodyBytes()

	dummyReq, _ := http.NewRequest(req.Method(), req.URL(), bytes.NewReader(body)) //nolint:noctx
	if dummyReq != nil {
		curl := telemetry.CurlFromRequest(dummyReq, body)
		fmt.Fprintf(os.Stderr, "%s\n", curl)
	}
}

// WithTrace constructs an [RequestModifier] assigning a connection tracer container to capture connection metrics.
func WithTrace(target *telemetry.TraceInfo) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).TraceInfo = target
	})
}

// WithTraceJA4 constructs an [RequestModifier] enabling JA4/JA4H client fingerprint telemetry.
func WithTraceJA4(target *telemetry.TraceInfo) RequestModifier {
	return Custom(func(req Request) {
		if target.JA4 == nil {
			target.JA4 = &ja4.Report{}
		}

		store := &pipeline.JA4ReportStore{Report: target.JA4, Target: target}
		getOrInitRequestConfig(req).JA4ReportStore = store

		if stdReq := req.HTTPRequest(); stdReq != nil {
			target.JA4.JA4H = telemetry.ComputeJA4HFromRequest(stdReq)
		}
	})
}

// WithTraceContext constructs an [RequestModifier] attaching a new [telemetry.TraceInfo] container to the request context.
func WithTraceContext() RequestModifier {
	return Custom(func(req Request) {
		info := &telemetry.TraceInfo{}
		getOrInitRequestConfig(req).TraceInfo = info
		WithTraceJA4(info).Apply(req)
	})
}

// WithJA4Callback constructs an [RequestModifier] setting a callback executed with the computed [ja4.Report] after TLS handshakes.
func WithJA4Callback(fn func(ja4.Report)) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).JA4Callback = fn
	})
}
