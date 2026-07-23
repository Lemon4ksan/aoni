// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package aoni

import (
	"context"
	stdio "io"
	"net/http"
)

// Request defines the unified, engine-agnostic HTTP request interface.
//
// It provides both string-based and byte-based accessors to ensure zero-allocation
// operations for high-performance engines while remaining 100% compatible with standard net/http.
type Request interface {
	// Context returns the execution context associated with the request.
	Context() context.Context

	// SetContext updates the execution context of the request.
	SetContext(ctx context.Context)

	// Method returns the HTTP method (e.g. "GET", "POST").
	Method() string

	// SetMethod sets the HTTP method using a string.
	SetMethod(method string)

	// SetMethodBytes sets the HTTP method using a byte slice without string allocation.
	SetMethodBytes(method []byte)

	// URL returns the full target URL string.
	URL() string

	// SetURL sets the full destination address using a string.
	SetURL(urlStr string)

	// SetURIBytes sets the full destination address using a byte slice.
	SetURIBytes(uri []byte)

	// Path returns the path component of the URI.
	Path() string

	// SetPath sets the path component of the URI.
	SetPath(path string)

	// RawQuery returns the raw URL query string.
	RawQuery() string

	// SetRawQuery sets the raw URL query string.
	SetRawQuery(query string)

	// SetRawQueryBytes sets the raw URL query string using a byte slice.
	SetRawQueryBytes(query []byte)

	// AddQueryParam appends a query key-value pair to the URI.
	AddQueryParam(key, value string)

	// AddQueryParamBytes appends a query key-value pair using byte slices.
	AddQueryParamBytes(key, value []byte)

	// SetQueryParam sets or replaces a query key-value pair in the URI.
	SetQueryParam(key, value string)

	// SetQueryParamBytes sets or replaces a query key-value pair using byte slices.
	SetQueryParamBytes(key, value []byte)

	// Header returns the single string value associated with key.
	Header(key string) string

	// HeaderBytes returns the value associated with key as a byte slice.
	HeaderBytes(key []byte) []byte

	// SetHeader sets the header key to value.
	SetHeader(key, value string)

	// SetHeaderBytes sets the header key to value using byte slices.
	SetHeaderBytes(key, value []byte)

	// AddHeader appends value to header key.
	AddHeader(key, value string)

	// AddHeaderBytes appends value to header key using byte slices.
	AddHeaderBytes(key, value []byte)

	// DelHeader removes the header key.
	DelHeader(key string)

	// DelHeaderBytes removes the header key using a byte slice.
	DelHeaderBytes(key []byte)

	// ResetHeaders removes all headers from the request.
	ResetHeaders()

	// SetBodyBytes sets the request body to a raw byte slice.
	SetBodyBytes(body []byte)

	// BodyBytes returns the request body as a byte slice if stored in memory.
	BodyBytes() []byte

	// SetBodyStream sets a streaming body reader with an optional content length (-1 if unknown).
	SetBodyStream(r stdio.Reader, contentLength int64)

	// BodyStream returns an io.Reader for the request body.
	BodyStream() stdio.Reader

	// HTTPRequest returns the underlying *http.Request if available (returns nil for non-net/http engines).
	HTTPRequest() *http.Request

	// EngineRequest returns the underlying raw request object of the active engine.
	EngineRequest() any
}

// Response defines the unified, engine-agnostic HTTP response interface.
type Response interface {
	// StatusCode returns the response HTTP status code.
	StatusCode() int

	// Status returns the response status string (e.g. "200 OK").
	Status() string

	// StatusBytes returns the response status line as a byte slice.
	StatusBytes() []byte

	// Header returns the single value for response header key.
	Header(key string) string

	// HeaderBytes returns the response header value as a byte slice.
	HeaderBytes(key []byte) []byte

	// Headers returns all response headers as a key-value map.
	Headers() map[string][]string

	// BodyBytes returns direct access to the response body byte slice.
	// For fast engines, this returns the internal socket buffer directly without memory copying.
	BodyBytes() []byte

	// BodyStream returns an io.ReadCloser for reading streaming responses.
	BodyStream() stdio.ReadCloser

	// HTTPResponse returns the underlying *http.Response if available (returns nil for non-net/http engines).
	HTTPResponse() *http.Response

	// EngineResponse returns the underlying raw response object of the active engine.
	EngineResponse() any

	// Close releases any resources associated with the response body.
	Close() error
}

// RequestDoer represents an engine capable of executing unified [Request] objects,
// satisfied by both [aoni.Client] and [fast.Client].
type RequestDoer interface {
	Do(req Request) (Response, error)
}
