// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"context"
	stdio "io"
	"net/http"
	"net/url"
	"strings"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

// StdRequest adapts a standard net/http [*http.Request] to the unified [Request] contract.
//
// Designed as a lightweight value wrapper to enable compiler inlining and stack allocation.
type StdRequest struct {
	req *http.Request
}

// NewStdRequest wraps req into a unified [Request] adapter.
//
// Preconditions: If req is nil, instantiates an empty request with initialized headers.
func NewStdRequest(req *http.Request) *StdRequest {
	if req == nil {
		req = &http.Request{Header: make(http.Header)}
	}

	return &StdRequest{req: req}
}

// Context returns the request execution context.
func (s *StdRequest) Context() context.Context {
	return s.req.Context()
}

// SetContext updates the request execution context in place.
func (s *StdRequest) SetContext(ctx context.Context) {
	if s.req != nil {
		*s.req = *s.req.WithContext(ctx)
	}
}

// Method returns the HTTP method string.
func (s *StdRequest) Method() string {
	return s.req.Method
}

// SetMethod updates the HTTP method string.
func (s *StdRequest) SetMethod(method string) {
	s.req.Method = method
}

// SetMethodBytes updates the HTTP method from a byte slice without heap allocations.
func (s *StdRequest) SetMethodBytes(method []byte) {
	s.req.Method = bytesconv.B2S(method)
}

// URL returns the full target URL string, or empty string if URL is nil.
func (s *StdRequest) URL() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.String()
}

// SetURL parses and assigns the destination address string to the request.
func (s *StdRequest) SetURL(urlStr string) {
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
func (s *StdRequest) SetURIBytes(uri []byte) {
	s.SetURL(bytesconv.B2S(uri))
}

// Path returns the path component of the target URL.
func (s *StdRequest) Path() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.Path
}

// SetPath updates the path component of the target URL.
func (s *StdRequest) SetPath(path string) {
	if s.req.URL == nil {
		return
	}

	s.req.URL.Path = path
	if s.req.URL.RawPath != "" {
		s.req.URL.RawPath = path
	}
}

// RawQuery returns the unescaped raw URL query string.
func (s *StdRequest) RawQuery() string {
	if s.req.URL == nil {
		return ""
	}

	return s.req.URL.RawQuery
}

// SetRawQuery overrides the raw URL query string.
func (s *StdRequest) SetRawQuery(query string) {
	if s.req.URL != nil {
		s.req.URL.RawQuery = query
	}
}

// SetRawQueryBytes overrides the raw URL query string from a byte slice.
func (s *StdRequest) SetRawQueryBytes(query []byte) {
	s.SetRawQuery(bytesconv.B2S(query))
}

// AddQueryParam appends an escaped key-value pair to the URL RawQuery string.
func (s *StdRequest) AddQueryParam(key, value string) {
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
func (s *StdRequest) AddQueryParamBytes(key, value []byte) {
	s.AddQueryParam(bytesconv.B2S(key), bytesconv.B2S(value))
}

// SetQueryParam sets or replaces a query parameter in the URL.
func (s *StdRequest) SetQueryParam(key, value string) {
	if s.req.URL == nil {
		return
	}

	q := s.req.URL.Query()
	q.Set(key, value)
	s.req.URL.RawQuery = q.Encode()
}

// SetQueryParamBytes sets or replaces a query parameter using byte slices.
func (s *StdRequest) SetQueryParamBytes(key, value []byte) {
	s.SetQueryParam(bytesconv.B2S(key), bytesconv.B2S(value))
}

// Header returns the single primary value associated with header key.
func (s *StdRequest) Header(key string) string {
	if s.req.Header == nil {
		return ""
	}

	return s.req.Header.Get(key)
}

// HeaderBytes returns header key value as a byte slice without allocations.
func (s *StdRequest) HeaderBytes(key []byte) []byte {
	return bytesconv.S2B(s.Header(bytesconv.B2S(key)))
}

// SetHeader overrides header key with value.
func (s *StdRequest) SetHeader(key, value string) {
	if s.req.Header == nil {
		s.req.Header = make(http.Header)
	}

	s.req.Header.Set(key, value)
}

// SetHeaderBytes overrides header key with value using byte slices.
func (s *StdRequest) SetHeaderBytes(key, value []byte) {
	s.SetHeader(bytesconv.B2S(key), bytesconv.B2S(value))
}

// AddHeader appends value to header key.
func (s *StdRequest) AddHeader(key, value string) {
	if s.req.Header == nil {
		s.req.Header = make(http.Header)
	}

	s.req.Header.Add(key, value)
}

// AddHeaderBytes appends value to header key using byte slices.
func (s *StdRequest) AddHeaderBytes(key, value []byte) {
	s.AddHeader(bytesconv.B2S(key), bytesconv.B2S(value))
}

// DelHeader removes header key.
func (s *StdRequest) DelHeader(key string) {
	if s.req.Header != nil {
		s.req.Header.Del(key)
	}
}

// DelHeaderBytes removes header key using a byte slice.
func (s *StdRequest) DelHeaderBytes(key []byte) {
	s.DelHeader(bytesconv.B2S(key))
}

// ResetHeaders clears all request headers.
func (s *StdRequest) ResetHeaders() {
	s.req.Header = make(http.Header)
}

// SetBodyBytes sets request body to a raw byte slice and configures replayable GetBody closure.
func (s *StdRequest) SetBodyBytes(body []byte) {
	s.req.Body = stdio.NopCloser(bytes.NewReader(body))
	s.req.ContentLength = int64(len(body))

	s.req.GetBody = func() (stdio.ReadCloser, error) {
		return stdio.NopCloser(bytes.NewReader(body)), nil
	}
}

// BodyBytes reads and returns the full request body byte payload.
func (s *StdRequest) BodyBytes() []byte {
	if s.req.Body == nil || s.req.Body == http.NoBody {
		return nil
	}

	b, err := stdio.ReadAll(s.req.Body)
	if err != nil {
		return nil
	}

	_ = s.req.Body.Close()
	s.req.Body = stdio.NopCloser(bytes.NewReader(b))

	return b
}

// SetBodyStream assigns a streaming reader as request body with specified Content-Length.
func (s *StdRequest) SetBodyStream(r stdio.Reader, contentLength int64) {
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
func (s *StdRequest) BodyStream() stdio.Reader {
	return s.req.Body
}

// HTTPRequest yields the underlying standard [*http.Request].
func (s *StdRequest) HTTPRequest() *http.Request {
	return s.req
}

// EngineRequest yields the underlying request cast to any.
func (s *StdRequest) EngineRequest() any {
	return s.req
}

// StdResponse adapts a standard net/http [*http.Response] to the unified [Response] contract.
type StdResponse struct {
	resp *http.Response
	body []byte
}

// NewStdResponse wraps resp into a unified [Response] adapter.
func NewStdResponse(resp *http.Response) *StdResponse {
	return &StdResponse{resp: resp}
}

// StatusCode returns the HTTP response status code, or 0 if response is nil.
func (s *StdResponse) StatusCode() int {
	if s.resp == nil {
		return 0
	}

	return s.resp.StatusCode
}

// Status returns response status text string.
func (s *StdResponse) Status() string {
	if s.resp == nil {
		return ""
	}

	return s.resp.Status
}

// StatusBytes returns response status text as a byte slice without allocations.
func (s *StdResponse) StatusBytes() []byte {
	return bytesconv.S2B(s.Status())
}

// Header returns single string value for header key.
func (s *StdResponse) Header(key string) string {
	if s.resp == nil || s.resp.Header == nil {
		return ""
	}

	return s.resp.Header.Get(key)
}

// HeaderBytes returns single header value as a byte slice without allocations.
func (s *StdResponse) HeaderBytes(key []byte) []byte {
	return bytesconv.S2B(s.Header(bytesconv.B2S(key)))
}

// Headers returns all response headers map.
func (s *StdResponse) Headers() map[string][]string {
	if s.resp == nil {
		return nil
	}

	return s.resp.Header
}

// BodyBytes reads, caches, and returns response body bytes.
func (s *StdResponse) BodyBytes() []byte {
	if s.body != nil {
		return s.body
	}

	if s.resp == nil || s.resp.Body == nil {
		return nil
	}

	b, err := stdio.ReadAll(s.resp.Body)
	if err != nil {
		return nil
	}

	_ = s.resp.Body.Close()
	s.body = b
	s.resp.Body = stdio.NopCloser(bytes.NewReader(b))

	return b
}

// BodyStream yields response body stream [stdio.ReadCloser].
func (s *StdResponse) BodyStream() stdio.ReadCloser {
	if s.resp == nil {
		return nil
	}

	return s.resp.Body
}

// HTTPResponse yields the underlying standard [*http.Response].
func (s *StdResponse) HTTPResponse() *http.Response {
	return s.resp
}

// EngineResponse yields the underlying response cast to any.
func (s *StdResponse) EngineResponse() any {
	return s.resp
}

// Close closes response body stream.
func (s *StdResponse) Close() error {
	if s.resp != nil && s.resp.Body != nil {
		return s.resp.Body.Close()
	}

	return nil
}
