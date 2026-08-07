// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"bytes"
	"io"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

var (
	responseAdapterPool = sync.Pool{
		New: func() any { return &Response{} },
	}
	pooledResponsePool = sync.Pool{
		New: func() any { return &PooledResponse{Response: &Response{}} },
	}
)

type fastBodyReadCloser struct {
	io.Reader
	fastReq  *fasthttp.Request
	fastResp *fasthttp.Response
	once     sync.Once
}

func (b *fastBodyReadCloser) Close() error {
	b.once.Do(func() {
		fasthttp.ReleaseRequest(b.fastReq)
		fasthttp.ReleaseResponse(b.fastResp)
	})

	return nil
}

// Response adapts a high-performance [*fasthttp.Response] to the unified [aoni.Response] contract.
type Response struct {
	resp         *fasthttp.Response
	trailers     map[string][]string
	uncompressed bool
}

// NewResponse wraps resp into a unified [aoni.Response] adapter.
// The caller is responsible for releasing the response object.
func NewResponse(resp *fasthttp.Response) *Response {
	if resp == nil {
		resp = fasthttp.AcquireResponse()
	}

	r := responseAdapterPool.Get().(*Response)
	r.resp = resp
	r.trailers = nil
	r.uncompressed = false

	return r
}

// SetTrailers registers HTTP trailers captured during frame execution.
func (f *Response) SetTrailers(trailers map[string][]string) {
	f.trailers = trailers
}

// Trailers returns HTTP trailers parsed after the body stream.
func (f *Response) Trailers() map[string][]string {
	return f.trailers
}

// StatusCode yields the HTTP status code.
func (f *Response) StatusCode() int {
	return f.resp.StatusCode()
}

// Status yields the response status text.
func (f *Response) Status() string {
	return http.StatusText(f.resp.StatusCode())
}

// StatusBytes yields status text as a byte slice.
func (f *Response) StatusBytes() []byte {
	return bytesconv.S2B(f.Status())
}

// Header yields single value for header key as a string.
func (f *Response) Header(key string) string {
	val := f.resp.Header.Peek(key)
	if len(val) == 0 {
		val = f.resp.Header.Peek(http.CanonicalHeaderKey(key))
	}

	if len(val) == 0 {
		f.resp.Header.All()(func(k, v []byte) bool {
			if bytesconv.EqualFoldASCII(bytesconv.B2S(k), key) {
				val = v
				return false
			}

			return true
		})
	}

	return bytesconv.B2S(val)
}

// HeaderBytes yields direct access to header value byte slice inside internal buffers.
func (f *Response) HeaderBytes(key []byte) []byte {
	val := f.resp.Header.PeekBytes(key)
	if len(val) == 0 {
		val = f.resp.Header.Peek(http.CanonicalHeaderKey(bytesconv.B2S(key)))
	}

	return val
}

// Headers yields all response headers as a key-value map.
func (f *Response) Headers() map[string][]string {
	m := make(map[string][]string)
	f.resp.Header.All()(func(k, v []byte) bool {
		sk := http.CanonicalHeaderKey(string(k))
		m[sk] = append(m[sk], string(v))
		return true
	})

	return m
}

// BodyBytes returns an independent, memory-safe copy of the response body bytes.
//
// Postconditions:
//   - The returned slice is safe to retain or mutate beyond response pool recycling.
func (f *Response) BodyBytes() []byte {
	return slices.Clone(f.resp.Body())
}

// UnsafeBodyBytes provides zero-allocation direct access to internal response buffers.
//
// Warning:
//   - Points directly to volatile internal buffers managed by sync.Pool.
//   - MUST NOT be referenced, mutated, or retained after closing or recycling the response.
func (f *Response) UnsafeBodyBytes() []byte {
	if f.resp == nil {
		return nil
	}

	return f.resp.Body()
}

// BodyStream yields an io.ReadCloser wrapping the response body stream or bytes.
func (f *Response) BodyStream() io.ReadCloser {
	if f.resp.IsBodyStream() {
		stream := f.resp.BodyStream()
		if rc, ok := stream.(io.ReadCloser); ok {
			return rc
		}

		return io.NopCloser(stream)
	}

	return io.NopCloser(bytes.NewReader(f.BodyBytes()))
}

// HTTPResponse yields nil for fasthttp response adapters.
func (f *Response) HTTPResponse() *http.Response {
	return nil
}

// FastHTTPResponse yields the underlying [*fasthttp.Response] instance.
func (f *Response) FastHTTPResponse() *fasthttp.Response {
	return f.resp
}

// EngineResponse yields the underlying [*fasthttp.Response] cast to any.
func (f *Response) EngineResponse() any {
	return f.resp
}

// SetUncompressed records whether the response payload was transparently decompressed by the client.
func (f *Response) SetUncompressed(v bool) {
	f.uncompressed = v
}

// Uncompressed reports whether the response body was transparently decompressed by the client.
func (f *Response) Uncompressed() bool {
	return f.uncompressed
}

// Release returns the Response adapter back to memory pool.
func (f *Response) Release() {
	if f == nil {
		return
	}

	f.resp = nil
	f.trailers = nil
	f.uncompressed = false
	responseAdapterPool.Put(f)
}

const maxBodySlurpBytes int64 = 2048

// Close releases resources bound to the response wrapper and slurps unread stream bytes to preserve sockets.
func (f *Response) Close() error {
	if f.resp != nil && f.resp.IsBodyStream() {
		if stream := f.resp.BodyStream(); stream != nil {
			_, _ = io.CopyN(io.Discard, stream, maxBodySlurpBytes)
		}
	}

	return nil
}

// PooledResponse wraps a fasthttp response and returns instances back to [sync.Pool] upon Close.
type PooledResponse struct {
	*Response
	fastReq  *fasthttp.Request
	fastResp *fasthttp.Response
	closed   atomic.Bool
}

// NewPooledResponse acquires a pooled PooledResponse adapter wrapping fastReq and fastResp.
// The caller is responsible for releasing the request and response objects.
func NewPooledResponse(fastReq *fasthttp.Request, fastResp *fasthttp.Response) *PooledResponse {
	pr := pooledResponsePool.Get().(*PooledResponse)
	if pr.Response == nil {
		pr.Response = &Response{}
	}

	pr.resp = fastResp
	pr.trailers = nil
	pr.uncompressed = false
	pr.fastReq = fastReq
	pr.fastResp = fastResp
	pr.closed.Store(false)

	return pr
}

// Close releases underlying fasthttp objects and returns PooledResponse to memory pool.
func (r *PooledResponse) Close() error {
	if r.closed.CompareAndSwap(false, true) {
		if r.fastReq != nil {
			fasthttp.ReleaseRequest(r.fastReq)
			r.fastReq = nil
		}

		if r.fastResp != nil {
			fasthttp.ReleaseResponse(r.fastResp)
			r.fastResp = nil
		}

		r.resp = nil
		r.trailers = nil
		r.uncompressed = false
		pooledResponsePool.Put(r)
	}

	return nil
}

var (
	_ aoni.Response = (*Response)(nil)
	_ aoni.Response = (*PooledResponse)(nil)
)
