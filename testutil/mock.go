// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package testutil provides zero-network mock execution engines and testing utilities for aoni.
package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/lemon4ksan/foundation/generic"
)

// MockEngine is an in-memory, zero-network mock HTTP doer for unit testing aoni applications.
type MockEngine struct {
	routes generic.Safe[[]*MockRoute]
}

// NewMockEngine creates a new in-memory [MockEngine].
func NewMockEngine() *MockEngine {
	return &MockEngine{}
}

// On registers a route expectation matching method and path.
func (m *MockEngine) On(method, path string) *MockRoute {
	route := &MockRoute{
		method:     method,
		path:       path,
		statusCode: http.StatusOK,
		headers:    make(http.Header),
	}

	m.routes.Mutate(func(routes *[]*MockRoute) {
		*routes = append(*routes, route)
	})

	return route
}

// Do executes the mock request matching against registered routes.
func (m *MockEngine) Do(req *http.Request) (*http.Response, error) {
	var matchedRoute *MockRoute

	m.routes.Read(func(routes []*MockRoute) {
		for _, route := range routes {
			if route.matches(req) {
				matchedRoute = route
				break
			}
		}
	})

	if matchedRoute != nil {
		return matchedRoute.respond(req)
	}

	return nil, fmt.Errorf("aoni/testutil: unexpected request: %s %s", req.Method, req.URL.String())
}

// MockRoute defines an expected HTTP route and configured response.
type MockRoute struct {
	method     string
	path       string
	statusCode int
	headers    http.Header
	body       []byte
	calls      int
}

// Reply sets the HTTP response status code for this route.
func (r *MockRoute) Reply(statusCode int) *MockRoute {
	r.statusCode = statusCode
	return r
}

// Header sets a response header on this route.
func (r *MockRoute) Header(key, value string) *MockRoute {
	r.headers.Set(key, value)
	return r
}

// JSON serializes payload as JSON and sets Content-Type to application/json.
func (r *MockRoute) JSON(payload any) *MockRoute {
	data, _ := json.Marshal(payload)
	r.body = data
	r.headers.Set("Content-Type", "application/json")

	return r
}

// String sets raw string body with text/plain.
func (r *MockRoute) String(text string) *MockRoute {
	r.body = []byte(text)
	r.headers.Set("Content-Type", "text/plain; charset=utf-8")

	return r
}

// Bytes sets raw byte slice response payload.
func (r *MockRoute) Bytes(data []byte) *MockRoute {
	r.body = data
	return r
}

// Calls returns the number of times this route was matched.
func (r *MockRoute) Calls() int {
	return r.calls
}

func (r *MockRoute) matches(req *http.Request) bool {
	if r.method != "" && r.method != req.Method {
		return false
	}

	if r.path != "" && r.path != req.URL.Path && r.path != req.URL.String() {
		return false
	}

	return true
}

func (r *MockRoute) respond(req *http.Request) (*http.Response, error) {
	r.calls++

	resp := &http.Response{
		StatusCode:    r.statusCode,
		Status:        fmt.Sprintf("%d %s", r.statusCode, http.StatusText(r.statusCode)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        r.headers.Clone(),
		Body:          io.NopCloser(bytes.NewReader(r.body)),
		ContentLength: int64(len(r.body)),
		Request:       req,
	}

	return resp, nil
}
