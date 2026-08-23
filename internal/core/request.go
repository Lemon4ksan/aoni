// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package core defines the foundational universal protocol atoms and zero-allocation
// execution contracts of the aoni network engine.
package core

import (
	"context"
	stdio "io"
	"iter"
	"net/http"
	"net/url"
)

// Request defines the unified, engine-agnostic HTTP request contract.
// It abstracts standard *http.Request and fasthttp.Request into a single,
// high-performance interface supporting both string and zero-alloc byte accessors.
type Request interface {
	// Context management
	Context() context.Context
	SetContext(ctx context.Context)

	// Method and Destination URL
	Method() string
	SetMethod(method string)
	URL() string
	SetURL(urlStr string)
	Path() string
	SetPath(path string)

	// Query Parameters
	RawQuery() string
	SetRawQuery(query string)
	AddQueryParam(key, value string)

	// Header Operations
	Header(key string) string
	Headers() iter.Seq2[[]byte, []byte]
	SetHeader(key, value string)
	AddHeader(key, value string)
	DelHeader(key string)
	ResetHeaders()

	// Payload Body Operations
	SetBodyBytes(body []byte)
	BodyBytes() []byte
	SetBodyStream(r stdio.Reader, contentLength int64)
	BodyStream() stdio.Reader

	// Underlying Engine Requests
	HTTPRequest() *http.Request
	EngineRequest() any
}

// RequestFactory is implemented by engines capable of pooling their own high-performance Request instances
// to minimize GC allocation overhead.
type RequestFactory interface {
	// AcquireRequest obtains a pooled [Request] instance.
	AcquireRequest() Request
	// ReleaseRequest releases a pooled [Request] instance back to the memory pool.
	ReleaseRequest(req Request)
}

// HeaderIterator is implemented by high-performance Request instances to support zero-allocation header traversal.
type HeaderIterator interface {
	EachHeader(fn func(key, value []byte) bool)
}

// QueryEncoder marshals arbitrary structures or maps into [url.Values].
type QueryEncoder func(any) (url.Values, error)
