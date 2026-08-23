// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package std provides high-performance zero-allocation adapters between standard library net/http
// and the unified aoni core.Request / core.Response contracts.
package std

import (
	"bytes"
	"context"
	"io"
	"iter"
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

// NewRequest wraps req into a pooled [Request] adapter.
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

// SetQueryParam sets or replaces a URL query parameter key with value.
func (s *Request) SetQueryParam(key, value string) {
	if s.req.URL == nil {
		return
	}

	q := s.req.URL.Query()
	q.Set(key, value)
	s.req.URL.RawQuery = q.Encode()
}

// SetQueryParamBytes sets or replaces a URL query parameter from byte slices.
func (s *Request) SetQueryParamBytes(key, value []byte) {
	s.SetQueryParam(bytesconv.B2S(key), bytesconv.B2S(value))
}

// Header returns the single string value for header key.
func (s *Request) Header(key string) string {
	if s.req.Header == nil {
		return ""
	}

	return s.req.Header.Get(key)
}

// HeaderBytes returns the header value for key from a byte slice.
func (s *Request) HeaderBytes(key []byte) []byte {
	return bytesconv.S2B(s.Header(bytesconv.B2S(key)))
}

// Headers yields every header key-value pair in the request.
func (s *Request) Headers() iter.Seq2[[]byte, []byte] {
	return func(yield func([]byte, []byte) bool) {
		if s.req == nil || s.req.Header == nil {
			return
		}

		for k, vv := range s.req.Header {
			kB := bytesconv.S2B(k)
			for _, v := range vv {
				if !yield(kB, bytesconv.S2B(v)) {
					return
				}
			}
		}
	}
}

// SetHeader sets or overrides the header value for key.
func (s *Request) SetHeader(key, value string) {
	if s.req.Header == nil {
		s.req.Header = make(http.Header)
	}

	if strings.EqualFold(key, "Host") {
		s.req.Host = value
	}

	s.req.Header.Set(key, value)
}

// SetHeaderBytes sets or overrides the header value using byte slices.
func (s *Request) SetHeaderBytes(key, value []byte) {
	s.SetHeader(bytesconv.B2S(key), bytesconv.B2S(value))
}

// AddHeader appends an additional header value for key.
func (s *Request) AddHeader(key, value string) {
	if s.req.Header == nil {
		s.req.Header = make(http.Header)
	}

	s.req.Header.Add(key, value)
}

// AddHeaderBytes appends an additional header value using byte slices.
func (s *Request) AddHeaderBytes(key, value []byte) {
	s.AddHeader(bytesconv.B2S(key), bytesconv.B2S(value))
}

// DelHeader removes all values associated with key from the header map.
func (s *Request) DelHeader(key string) {
	if s.req.Header != nil {
		s.req.Header.Del(key)
	}
}

// DelHeaderBytes removes all values associated with key from a byte slice.
func (s *Request) DelHeaderBytes(key []byte) {
	s.DelHeader(bytesconv.B2S(key))
}

// ResetHeaders clears all headers from the request.
func (s *Request) ResetHeaders() {
	s.req.Header = make(http.Header)
}

// SetBodyBytes sets the request body to body.
func (s *Request) SetBodyBytes(body []byte) {
	s.req.Body = io.NopCloser(bytes.NewReader(body))
	s.req.ContentLength = int64(len(body))
}

// BodyBytes returns the request payload bytes.
func (s *Request) BodyBytes() []byte {
	if s.req.Body == nil {
		return nil
	}

	b, _ := io.ReadAll(s.req.Body)
	s.req.Body = io.NopCloser(bytes.NewReader(b))

	return b
}

// SetBodyStream configures an arbitrary io.Reader stream as the request body.
func (s *Request) SetBodyStream(r io.Reader, contentLength int64) {
	if rc, ok := r.(io.ReadCloser); ok {
		s.req.Body = rc
	} else if r != nil {
		s.req.Body = io.NopCloser(r)
	}

	s.req.ContentLength = contentLength
}

// BodyStream returns the underlying [io.Reader] stream.
func (s *Request) BodyStream() io.Reader {
	return s.req.Body
}

// HTTPRequest yields the adapted [*http.Request] instance.
func (s *Request) HTTPRequest() *http.Request {
	return s.req
}

// EngineRequest returns the underlying [*http.Request] pointer.
func (s *Request) EngineRequest() any {
	return s.req
}

var _ core.Request = (*Request)(nil)
