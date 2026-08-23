// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package std

import (
	"bytes"
	"io"
	"net/http"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/pool"

	"github.com/lemon4ksan/aoni/internal/core"
)

var responseStorage = pool.NewPerPStorage(func() *Response {
	return &Response{}
})

// Response adapts a standard net/http [*http.Response] to the unified [core.Response] contract.
type Response struct {
	resp *http.Response
	body []byte
}

// NewResponse wraps resp into a unified [spec.Response] adapter.
//
// Postconditions:
//   - The returned response must be released via [ReleaseResponse] to prevent pool leaks.
func NewResponse(resp *http.Response) *Response {
	r := responseStorage.Get()
	r.resp = resp
	r.body = nil

	return r
}

// ReleaseResponse returns the response to the pool after execution.
func ReleaseResponse(r *Response) {
	if r == nil {
		return
	}

	r.resp = nil
	r.body = nil
	responseStorage.Put(r)
}

// StatusCode returns the HTTP response status code, or 0 if response is nil.
func (s *Response) StatusCode() int {
	if s.resp == nil {
		return 0
	}

	return s.resp.StatusCode
}

// Status returns response status text string.
func (s *Response) Status() string {
	if s.resp == nil {
		return ""
	}

	return s.resp.Status
}

// StatusBytes returns response status text as a byte slice without allocations.
func (s *Response) StatusBytes() []byte {
	return bytesconv.S2B(s.Status())
}

// Header returns single string value for header key.
func (s *Response) Header(key string) string {
	if s.resp == nil || s.resp.Header == nil {
		return ""
	}

	return s.resp.Header.Get(key)
}

// HeaderBytes returns single header value as a byte slice without allocations.
func (s *Response) HeaderBytes(key []byte) []byte {
	return bytesconv.S2B(s.Header(bytesconv.B2S(key)))
}

// Headers returns all response headers map.
func (s *Response) Headers() map[string][]string {
	if s.resp == nil {
		return nil
	}

	return s.resp.Header
}

// Trailers returns HTTP response trailers attached to the underlying response.
func (s *Response) Trailers() map[string][]string {
	if s.resp == nil || s.resp.Trailer == nil {
		return nil
	}

	return s.resp.Trailer
}

// SetTrailers updates HTTP response trailers on the underlying response.
func (s *Response) SetTrailers(trailers map[string][]string) {
	if s.resp == nil {
		return
	}

	if s.resp.Trailer == nil {
		s.resp.Trailer = make(http.Header, len(trailers))
	}

	for k, vv := range trailers {
		for _, v := range vv {
			s.resp.Trailer.Add(k, v)
		}
	}
}

// BodyBytes reads, caches, and returns response body bytes.
func (s *Response) BodyBytes() []byte {
	if s.body != nil {
		return s.body
	}

	if s.resp == nil || s.resp.Body == nil {
		return nil
	}

	b, err := io.ReadAll(s.resp.Body)
	if err != nil {
		return nil
	}

	_ = s.resp.Body.Close()
	s.body = b
	s.resp.Body = io.NopCloser(bytes.NewReader(b))

	return b
}

// UnsafeBodyBytes provides direct access to cached response body bytes.
func (s *Response) UnsafeBodyBytes() []byte {
	return s.BodyBytes()
}

// UnsafeAccess provides explicit, zero-allocation access to volatile response buffers.
type UnsafeAccess struct {
	resp *Response
}

// Bytes returns a direct slice of cached response body memory.
func (u UnsafeAccess) Bytes() []byte {
	if u.resp == nil {
		return nil
	}

	return u.resp.UnsafeBodyBytes()
}

// String returns a string view over cached response body memory.
func (u UnsafeAccess) String() string {
	return string(u.Bytes())
}

// Unsafe returns an [UnsafeAccess] accessor for zero-copy operations.
func (s *Response) Unsafe() UnsafeAccess {
	return UnsafeAccess{resp: s}
}

// BodyStream yields response body stream [io.ReadCloser].
func (s *Response) BodyStream() io.ReadCloser {
	if s.resp == nil {
		return nil
	}

	return s.resp.Body
}

// HTTPResponse yields the underlying standard [*http.Response].
func (s *Response) HTTPResponse() *http.Response {
	return s.resp
}

// EngineResponse yields the underlying response cast to any.
func (s *Response) EngineResponse() any {
	return s.resp
}

// Uncompressed reports whether the response body was transparently decompressed by the client.
func (s *Response) Uncompressed() bool {
	if s.resp == nil {
		return false
	}

	return s.resp.Uncompressed
}

// SetUncompressed sets whether the response body was transparently decompressed.
func (s *Response) SetUncompressed(v bool) {
	if s.resp != nil {
		s.resp.Uncompressed = v
	}
}

// Close closes the response body stream and releases the response adapter to the pool.
func (s *Response) Close() error {
	var err error
	if s.resp != nil && s.resp.Body != nil {
		err = s.resp.Body.Close()
	}

	ReleaseResponse(s)

	return err
}

// Ensure Response implements core.Response.
var _ core.Response = (*Response)(nil)
