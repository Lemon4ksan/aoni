// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package otel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/lemon4ksan/aoni"
)

// mockRequest implements [aoni.Request] for unit testing.
type mockRequest struct {
	ctx     context.Context
	method  string
	url     string
	headers map[string]string
	body    []byte
}

func newMockRequest(method, targetURL string) *mockRequest {
	return &mockRequest{
		ctx:     context.Background(),
		method:  method,
		url:     targetURL,
		headers: make(map[string]string),
	}
}

func (r *mockRequest) Context() context.Context {
	if r.ctx == nil {
		return context.Background()
	}
	return r.ctx
}
func (r *mockRequest) SetContext(ctx context.Context)     { r.ctx = ctx }
func (r *mockRequest) Method() string                     { return r.method }
func (r *mockRequest) SetMethod(m string)                 { r.method = m }
func (r *mockRequest) URL() string                        { return r.url }
func (r *mockRequest) SetURL(u string)                    { r.url = u }
func (r *mockRequest) Path() string                       { return "" }
func (r *mockRequest) SetPath(_ string)                   {}
func (r *mockRequest) RawQuery() string                   { return "" }
func (r *mockRequest) SetRawQuery(_ string)               {}
func (r *mockRequest) AddQueryParam(_, _ string)          {}
func (r *mockRequest) Header(k string) string             { return r.headers[k] }
func (r *mockRequest) SetHeader(k, v string)              { r.headers[k] = v }
func (r *mockRequest) AddHeader(k, v string)              { r.headers[k] = v }
func (r *mockRequest) DelHeader(k string)                 { delete(r.headers, k) }
func (r *mockRequest) ResetHeaders()                      { clear(r.headers) }
func (r *mockRequest) SetBodyBytes(b []byte)              { r.body = b }
func (r *mockRequest) BodyBytes() []byte                  { return r.body }
func (r *mockRequest) SetBodyStream(_ io.Reader, _ int64) {}
func (r *mockRequest) BodyStream() io.Reader              { return nil }
func (r *mockRequest) HTTPRequest() *http.Request         { return nil }
func (r *mockRequest) EngineRequest() any                 { return nil }
func (r *mockRequest) Headers() iter.Seq2[[]byte, []byte] {
	return func(yield func([]byte, []byte) bool) {
		for k, v := range r.headers {
			if !yield([]byte(k), []byte(v)) {
				return
			}
		}
	}
}

// mockResponse implements [aoni.Response] for unit testing.
type mockResponse struct {
	statusCode int
	statusText string
	body       []byte
}

func (r *mockResponse) StatusCode() int                   { return r.statusCode }
func (r *mockResponse) Status() string                    { return r.statusText }
func (r *mockResponse) StatusBytes() []byte               { return []byte(r.statusText) }
func (r *mockResponse) Header(_ string) string            { return "" }
func (r *mockResponse) HeaderBytes(_ []byte) []byte       { return nil }
func (r *mockResponse) Headers() map[string][]string      { return nil }
func (r *mockResponse) Trailers() map[string][]string     { return nil }
func (r *mockResponse) SetTrailers(_ map[string][]string) {}
func (r *mockResponse) BodyBytes() []byte                 { return r.body }
func (r *mockResponse) UnsafeBodyBytes() []byte           { return r.body }
func (r *mockResponse) BodyStream() io.ReadCloser         { return nil }
func (r *mockResponse) HTTPResponse() *http.Response      { return nil }
func (r *mockResponse) EngineResponse() any               { return nil }
func (r *mockResponse) Uncompressed() bool                { return false }
func (r *mockResponse) SetUncompressed(_ bool)            {}
func (r *mockResponse) Close() error                      { return nil }

func TestMiddleware_TraceparentInjection(t *testing.T) {
	memExp := NewMemoryExporter()
	tracer := NewTracer("test-service", WithExporter(memExp))

	mw := NewMiddleware(
		WithTracer(tracer),
		WithServiceName("test-service"),
	)

	req := newMockRequest("GET", "https://api.example.com/items/123")
	req.SetHeader("User-Agent", "aoni/1.0")

	doer := mw(aoni.DoerFunc(func(r aoni.Request) (aoni.Response, error) {
		traceParent := r.Header(HeaderTraceParent)
		if traceParent == "" {
			t.Errorf("expected traceparent header to be present on downstream request")
		}

		sc, err := ParseTraceParent(traceParent)
		if err != nil || !sc.IsValid() {
			t.Errorf("expected valid traceparent header, got %s (err: %v)", traceParent, err)
		}

		return &mockResponse{
			statusCode: 200,
			statusText: "200 OK",
			body:       []byte(`{"status":"ok"}`),
		}, nil
	}))

	resp, err := doer.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode())
	}

	spans := memExp.Spans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 exported span, got %d", len(spans))
	}

	span := spans[0]
	if span.Kind != SpanKindClient {
		t.Errorf("expected span kind CLIENT, got %v", span.Kind)
	}
	if span.Status != StatusOk {
		t.Errorf("expected status OK, got %v", span.Status)
	}

	// Verify Semantic Conventions
	hasMethod := false
	hasURL := false
	hasStatus := false

	for _, a := range span.Attributes {
		if a.Key == KeyHTTPRequestMethod && a.Value == "GET" {
			hasMethod = true
		}
		if a.Key == KeyURLFull && a.Value == "https://api.example.com/items/123" {
			hasURL = true
		}
		if a.Key == KeyHTTPResponseStatusCode && a.Value == 200 {
			hasStatus = true
		}
	}

	if !hasMethod || !hasURL || !hasStatus {
		t.Errorf("missing expected semconv attributes: method=%v, url=%v, status=%v", hasMethod, hasURL, hasStatus)
	}
}

func TestMiddleware_ErrorRecording(t *testing.T) {
	memExp := NewMemoryExporter()
	tracer := NewTracer("test-service", WithExporter(memExp))

	mw := NewMiddleware(WithTracer(tracer))

	expectedErr := errors.New("network dial timeout")
	req := newMockRequest("POST", "https://api.example.com/upload")
	req.SetBodyBytes([]byte("test payload"))

	doer := mw(aoni.DoerFunc(func(_ aoni.Request) (aoni.Response, error) {
		return nil, expectedErr
	}))

	_, err := doer.Do(req)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}

	spans := memExp.Spans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Status != StatusError {
		t.Errorf("expected span status ERROR, got %v", span.Status)
	}
	if !strings.Contains(span.StatusDescription, "network dial timeout") {
		t.Errorf("expected status description to contain error message, got %s", span.StatusDescription)
	}

	// Check exception event
	hasExceptionEvent := false
	for _, ev := range span.Events {
		if ev.Name == "exception" {
			hasExceptionEvent = true
		}
	}
	if !hasExceptionEvent {
		t.Errorf("expected exception event to be recorded on span")
	}
}

func TestMiddleware_Filter(t *testing.T) {
	memExp := NewMemoryExporter()
	tracer := NewTracer("test-service", WithExporter(memExp))

	mw := NewMiddleware(
		WithTracer(tracer),
		WithFilter(func(r aoni.Request) bool {
			return !strings.Contains(r.URL(), "/healthz")
		}),
	)

	doer := mw(aoni.DoerFunc(func(_ aoni.Request) (aoni.Response, error) {
		return &mockResponse{statusCode: 200}, nil
	}))

	// Health check request should be filtered out
	healthReq := newMockRequest("GET", "https://api.example.com/healthz")
	_, _ = doer.Do(healthReq)

	if len(memExp.Spans()) != 0 {
		t.Fatalf("expected 0 spans for filtered request, got %d", len(memExp.Spans()))
	}

	// Normal request should be traced
	normalReq := newMockRequest("GET", "https://api.example.com/api/v1/data")
	_, _ = doer.Do(normalReq)

	if len(memExp.Spans()) != 1 {
		t.Fatalf("expected 1 span for normal request, got %d", len(memExp.Spans()))
	}
}

func TestMiddleware_ConcurrentSafety(t *testing.T) {
	memExp := NewMemoryExporter()
	tracer := NewTracer("concurrent-service", WithExporter(memExp))
	mw := NewMiddleware(WithTracer(tracer))

	doer := mw(aoni.DoerFunc(func(r aoni.Request) (aoni.Response, error) {
		return &mockResponse{statusCode: 200, statusText: "200 OK"}, nil
	}))

	var wg sync.WaitGroup
	workers := 50
	iterations := 100

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				req := newMockRequest("GET", fmt.Sprintf("https://api.example.com/items/%d", workerID*iterations+j))
				resp, err := doer.Do(req)
				if err != nil || resp.StatusCode() != 200 {
					t.Errorf("worker %d failed at %d: %v", workerID, j, err)
					return
				}
			}
		}(i)
	}

	wg.Wait()

	spans := memExp.Spans()
	expectedTotal := workers * iterations
	if len(spans) != expectedTotal {
		t.Errorf("expected %d spans, got %d", expectedTotal, len(spans))
	}
}
