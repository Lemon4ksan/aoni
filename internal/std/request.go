// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package std provides high-performance zero-allocation adapters between standard library net/http
// and the unified aoni spec.Request / spec.Response contracts.
package std

import (
	"bytes"
	"context"
	stdio "io"
	"net/http"
	"net/url"
	"strings"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/pool"

	"github.com/lemon4ksan/aoni/internal/core"
)

var stdRequestStorage = pool.NewPerPStorage(func() *Request {
	return &Request{}
})

// Request adapts a standard net/http [*http.Request] to the unified [core.Request] contract.
type Request struct {
	req *http.Request
}

// NewRequest wraps req into a pooled [spec.Request] adapter.
//
// Postconditions:
//   - The returned request must be executed or released via [ReleaseRequest] to prevent pool leaks.
func NewRequest(req *http.Request) *Request {
	if req == nil {
		req = &http.Request{Header: make(http.Header)}
	}

	r := stdRequestStorage.Get()
	r.req = req

	return r
}

// ReleaseRequest returns the request to the pool after execution.
func ReleaseRequest(r *Request) {
	if r == nil {
		return
	}

	r.req = nil
	stdRequestStorage.Put(r)
}

// Context returns the request execution context.
func (s *Request) Context() context.Context {
	return s.req.Context()
}

// SetContext updates the request execution context in place.
func (s *Request) SetContext(ctx context.Context) {
	if s.req != nil {
		*s.req = *s.req.WithContext(ctx)
	}
}

// Method returns the HTTP method string.
func (s *Request) Method() string {
	return s.req.Method
}

// SetMethod updates the HTTP method string.
func (s *Request) SetMethod(method string) {
	s.req.Method = method
}

// SetMethodBytes updates the HTTP method from a byte slice without heap allocations.
func (s *Request) SetMethodBytes(method []byte) {
	s.req.Method = bytesconv.B2S(method)
}

// URL returns the full target URL string, or empty string if URL is nil.
func (s *Request) URL() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.String()
}

// SetURL parses and assigns the destination address string to the request.
func (s *Request) SetURL(urlStr string) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return
	}

	s.req.URL = u
	if u.Host != "" {
		s.req.Host = u.Host
	}
}

// SetURIBytes parses and assigns the destination address from a byte slice.
func (s *Request) SetURIBytes(uri []byte) {
	s.SetURL(bytesconv.B2S(uri))
}

// Path returns the path component of the target URL.
func (s *Request) Path() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.Path
}

// SetPath updates the path component of the target URL.
func (s *Request) SetPath(path string) {
	if s.req.URL == nil {
		return
	}

	s.req.URL.Path = path
	s.req.URL.RawPath = ""
}

// RawQuery returns the unescaped raw URL query string.
func (s *Request) RawQuery() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.RawQuery
}

// SetRawQuery overrides the raw URL query string.
func (s *Request) SetRawQuery(query string) {
	if s.req.URL != nil {
		s.req.URL.RawQuery = query
	}
}

// SetRawQueryBytes overrides the raw URL query string from a byte slice.
func (s *Request) SetRawQueryBytes(query []byte) {
	s.SetRawQuery(bytesconv.B2S(query))
}

// AddQueryParam appends an escaped key-value pair to the URL RawQuery string.
func (s *Request) AddQueryParam(key, value string) {
	if s.req.URL == nil {
		return
	}

	escapedKey := url.QueryEscape(key)
	escapedVal := url.QueryEscape(value)

	if s.req.URL.RawQuery == "" {
		s.req.URL.RawQuery = escapedKey + "=" + escapedVal
		return
	}

	var sb strings.Builder
	sb.Grow(len(s.req.URL.RawQuery) + len(escapedKey) + len(escapedVal) + 2)
	sb.WriteString(s.req.URL.RawQuery)
	sb.WriteByte('&')
	sb.WriteString(escapedKey)
	sb.WriteByte('=')
	sb.WriteString(escapedVal)

	s.req.URL.RawQuery = sb.String()
}

// AddQueryParamBytes appends an escaped key-value pair using byte slices.
func (s *Request) AddQueryParamBytes(key, value []byte) {
	s.AddQueryParam(bytesconv.B2S(key), bytesconv.B2S(value))
}

// SetQueryParam sets or replaces a query parameter in the URL.
func (s *Request) SetQueryParam(key, value string) {
	if s.req.URL == nil {
		return
	}

	q := s.req.URL.Query()
	q.Set(key, value)
	s.req.URL.RawQuery = q.Encode()
}

// SetQueryParamBytes sets or replaces a query parameter using byte slices.
func (s *Request) SetQueryParamBytes(key, value []byte) {
	s.SetQueryParam(bytesconv.B2S(key), bytesconv.B2S(value))
}

// Header returns the single primary value associated with header key.
func (s *Request) Header(key string) string {
	if s.req.Header == nil {
		return ""
	}

	return s.req.Header.Get(key)
}

// HeaderBytes returns header key value as a byte slice without allocations.
func (s *Request) HeaderBytes(key []byte) []byte {
	return bytesconv.S2B(s.Header(bytesconv.B2S(key)))
}

// SetHeader overrides header key with value.
func (s *Request) SetHeader(key, value string) {
	if s.req.Header == nil {
		s.req.Header = make(http.Header)
	}

	s.req.Header.Set(key, value)
}

// SetHeaderBytes overrides header key with value using byte slices.
func (s *Request) SetHeaderBytes(key, value []byte) {
	s.SetHeader(bytesconv.B2S(key), bytesconv.B2S(value))
}

// AddHeader appends value to header key.
func (s *Request) AddHeader(key, value string) {
	if s.req.Header == nil {
		s.req.Header = make(http.Header)
	}

	s.req.Header.Add(key, value)
}

// AddHeaderBytes appends value to header key using byte slices.
func (s *Request) AddHeaderBytes(key, value []byte) {
	s.AddHeader(bytesconv.B2S(key), bytesconv.B2S(value))
}

// DelHeader removes header key.
func (s *Request) DelHeader(key string) {
	if s.req.Header != nil {
		s.req.Header.Del(key)
	}
}

// DelHeaderBytes removes header key using a byte slice.
func (s *Request) DelHeaderBytes(key []byte) {
	s.DelHeader(bytesconv.B2S(key))
}

// ResetHeaders clears all request headers.
func (s *Request) ResetHeaders() {
	s.req.Header = make(http.Header)
}

// SetBodyBytes sets request body to a raw byte slice and configures replayable GetBody closure.
func (s *Request) SetBodyBytes(body []byte) {
	s.req.Body = stdio.NopCloser(bytes.NewReader(body))
	s.req.ContentLength = int64(len(body))

	s.req.GetBody = func() (stdio.ReadCloser, error) {
		return stdio.NopCloser(bytes.NewReader(body)), nil
	}
}

// BodyBytes reads and returns the full request body byte payload.
func (s *Request) BodyBytes() []byte {
	if s.req.Body == nil || s.req.Body == http.NoBody {
		return nil
	}

	bodyRC := s.req.Body
	if s.req.GetBody != nil {
		if b, err := s.req.GetBody(); err == nil && b != nil {
			bodyRC = b
		}
	}

	b, err := stdio.ReadAll(bodyRC)
	_ = bodyRC.Close()

	if err != nil {
		return nil
	}

	return b
}

// SetBodyStream assigns a streaming reader as request body with specified Content-Length.
func (s *Request) SetBodyStream(r stdio.Reader, contentLength int64) {
	if r == nil {
		s.req.Body = http.NoBody
		s.req.ContentLength = 0

		return
	}

	if rc, ok := r.(stdio.ReadCloser); ok {
		s.req.Body = rc
	} else {
		s.req.Body = stdio.NopCloser(r)
	}

	s.req.ContentLength = contentLength
}

// BodyStream yields the underlying request body [stdio.Reader].
func (s *Request) BodyStream() stdio.Reader {
	return s.req.Body
}

// HTTPRequest yields the underlying standard [*http.Request].
func (s *Request) HTTPRequest() *http.Request {
	return s.req
}

// EngineRequest yields the underlying request cast to any.
func (s *Request) EngineRequest() any {
	return s.req
}

// Ensure Request implements core.Request.
var _ core.Request = (*Request)(nil)
