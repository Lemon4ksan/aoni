// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import (
	"bytes"
	"context"
	stdio "io"
	"net/http"
	"net/url"
	"strings"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/pool"
)

var stdRequestStorage = pool.NewPerPStorage(func() *StdRequest {
	return &StdRequest{}
})

// StdRequest adapts a standard net/http [*http.Request] to the unified [Request] contract.
type StdRequest struct {
	req *http.Request
}

// NewStdRequest wraps req into a pooled [StdRequest] adapter.
func NewStdRequest(req *http.Request) *StdRequest {
	if req == nil {
		req = &http.Request{Header: make(http.Header)}
	}

	r := stdRequestStorage.Get()
	r.req = req

	return r
}

// ReleaseStdRequest returns the request to the pool after execution.
func ReleaseStdRequest(r *StdRequest) {
	if r == nil {
		return
	}

	r.req = nil
	stdRequestStorage.Put(r)
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
	s.req.URL.RawPath = ""
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

// SetQueryParam sets or replaces a URL query parameter key with value.
func (s *StdRequest) SetQueryParam(key, value string) {
	if s.req.URL == nil {
		return
	}

	q := s.req.URL.Query()
	q.Set(key, value)
	s.req.URL.RawQuery = q.Encode()
}

// SetQueryParamBytes sets or replaces a URL query parameter from byte slices.
func (s *StdRequest) SetQueryParamBytes(key, value []byte) {
	s.SetQueryParam(bytesconv.B2S(key), bytesconv.B2S(value))
}

// Header returns the single string value for header key.
func (s *StdRequest) Header(key string) string {
	if s.req.Header == nil {
		return ""
	}

	return s.req.Header.Get(key)
}

// HeaderBytes returns the header value for key from a byte slice.
func (s *StdRequest) HeaderBytes(key []byte) []byte {
	return bytesconv.S2B(s.Header(bytesconv.B2S(key)))
}

// SetHeader sets or overrides the header value for key.
func (s *StdRequest) SetHeader(key, value string) {
	if s.req.Header == nil {
		s.req.Header = make(http.Header)
	}

	if strings.EqualFold(key, "Host") {
		s.req.Host = value
	}

	s.req.Header.Set(key, value)
}

// SetHeaderBytes sets or overrides the header value using byte slices.
func (s *StdRequest) SetHeaderBytes(key, value []byte) {
	s.SetHeader(bytesconv.B2S(key), bytesconv.B2S(value))
}

// AddHeader appends an additional header value for key.
func (s *StdRequest) AddHeader(key, value string) {
	if s.req.Header == nil {
		s.req.Header = make(http.Header)
	}

	s.req.Header.Add(key, value)
}

// AddHeaderBytes appends an additional header value using byte slices.
func (s *StdRequest) AddHeaderBytes(key, value []byte) {
	s.AddHeader(bytesconv.B2S(key), bytesconv.B2S(value))
}

// DelHeader removes all values associated with key from the header map.
func (s *StdRequest) DelHeader(key string) {
	if s.req.Header != nil {
		s.req.Header.Del(key)
	}
}

// DelHeaderBytes removes all values associated with key from a byte slice.
func (s *StdRequest) DelHeaderBytes(key []byte) {
	s.DelHeader(bytesconv.B2S(key))
}

// ResetHeaders clears all headers from the request.
func (s *StdRequest) ResetHeaders() {
	s.req.Header = make(http.Header)
}

// SetBodyBytes sets the request body to body.
func (s *StdRequest) SetBodyBytes(body []byte) {
	s.req.Body = stdio.NopCloser(bytes.NewReader(body))
	s.req.ContentLength = int64(len(body))
}

// BodyBytes returns the request payload bytes.
func (s *StdRequest) BodyBytes() []byte {
	if s.req.Body == nil {
		return nil
	}

	b, _ := stdio.ReadAll(s.req.Body)
	s.req.Body = stdio.NopCloser(bytes.NewReader(b))

	return b
}

// SetBodyStream configures an arbitrary io.Reader stream as the request body.
func (s *StdRequest) SetBodyStream(r stdio.Reader, contentLength int64) {
	if rc, ok := r.(stdio.ReadCloser); ok {
		s.req.Body = rc
	} else if r != nil {
		s.req.Body = stdio.NopCloser(r)
	}

	s.req.ContentLength = contentLength
}

// BodyStream returns the underlying [stdio.Reader] stream.
func (s *StdRequest) BodyStream() stdio.Reader {
	return s.req.Body
}

// HTTPRequest yields the adapted [*http.Request] instance.
func (s *StdRequest) HTTPRequest() *http.Request {
	return s.req
}

// EngineRequest returns the underlying [*http.Request] pointer.
func (s *StdRequest) EngineRequest() any {
	return s.req
}

var _ Request = (*StdRequest)(nil)
