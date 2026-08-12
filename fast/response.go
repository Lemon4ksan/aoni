// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"bytes"
	"io"
	"net/http"
	"slices"
	"strconv"
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
		New: func() any { return &PooledResponse{} },
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
//
// Memory Lifetime Invariants & Thread Safety:
// Response instances are recycled via [sync.Pool]. Callers MUST call [Response.Close] or [Response.Release]
// when finished processing the response to avoid socket leaks and memory fragmentation.
type Response struct {
	resp         *fasthttp.Response
	trailers     map[string][]string
	uncompressed bool
}

// NewResponse acquires a pooled [Response] adapter wrapping an active [*fasthttp.Response].
// If resp is nil, a new [*fasthttp.Response] is acquired automatically from [fasthttp.AcquireResponse].
// Yields a zero-allocation adapter instance configured for pipeline processing.
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

// StatusCode yields the HTTP status code (e.g. 200, 404, 500).
func (f *Response) StatusCode() int {
	return f.resp.StatusCode()
}

// Status yields standard HTTP status text corresponding to StatusCode (e.g. "200 OK").
func (f *Response) Status() string {
	return http.StatusText(f.resp.StatusCode())
}

// StatusBytes yields status text as a zero-allocation byte slice.
func (f *Response) StatusBytes() []byte {
	return bytesconv.S2B(f.Status())
}

// Header yields a single value for key as a string, using case-insensitive header lookup.
func (f *Response) Header(key string) string {
	val := f.resp.Header.Peek(key)
	if len(val) == 0 {
		val = f.resp.Header.Peek(bytesconv.CanonicalHeaderKey(key))
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
//
// Memory Lifetime Warning:
//   - The returned byte slice points into internal buffer memory. It MUST NOT be retained beyond the response lifecycle.
func (f *Response) HeaderBytes(key []byte) []byte {
	val := f.resp.Header.PeekBytes(key)
	if len(val) == 0 {
		val = f.resp.Header.PeekBytes(bytesconv.CanonicalHeaderKeyBytes(key))
	}

	return val
}

// Headers yields all response headers as a canonical key-value map.
func (f *Response) Headers() map[string][]string {
	m := make(map[string][]string)
	f.resp.Header.All()(func(k, v []byte) bool {
		sk := bytesconv.CanonicalHeaderKey(bytesconv.B2S(k))
		m[sk] = append(m[sk], string(v))
		return true
	})

	return m
}

// SetTrailers registers HTTP trailers captured during frame execution.
func (f *Response) SetTrailers(trailers map[string][]string) {
	f.trailers = trailers
}

// Trailers returns HTTP trailers parsed after the body stream.
func (f *Response) Trailers() map[string][]string {
	return f.trailers
}

// BodyBytes returns an independent, memory-safe copy of the response body bytes.
// The returned slice is completely safe to retain or mutate beyond response pool recycling.
func (f *Response) BodyBytes() []byte {
	return slices.Clone(f.resp.Body())
}

// UnsafeBodyBytes provides zero-allocation direct access to internal response buffers.
//
// Critical Memory Lifetime Warning:
//   - Points directly into volatile internal buffers managed by [sync.Pool].
//   - Callers MUST NOT reference, mutate, or retain this byte slice beyond closing or releasing the response.
func (f *Response) UnsafeBodyBytes() []byte {
	if f.resp == nil {
		return nil
	}

	return f.resp.Body()
}

// BodyStream yields an [io.ReadCloser] wrapping the response body stream or bytes.
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

// HTTPResponse yields nil for fasthttp response adapters (standard net/http.Response unavailable).
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

// Release returns the Response adapter instance back to [sync.Pool] for memory recycling.
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

// Close releases resources bound to the response wrapper and slurps unread stream bytes to preserve Keep-Alive sockets.
func (f *Response) Close() error {
	if f.resp == nil || !f.resp.IsBodyStream() {
		return nil
	}

	stream := f.resp.BodyStream()
	if stream == nil {
		return nil
	}

	clStr := f.Header("Content-Length")
	slurpLimit := maxBodySlurpBytes

	if cl, err := strconv.ParseInt(clStr, 10, 64); err == nil && cl >= 0 {
		slurpLimit = min(cl, maxBodySlurpBytes)
	}

	_, _ = io.CopyN(io.Discard, stream, slurpLimit)

	return nil
}

// PooledResponse wraps a fasthttp request/response pair, automatically releasing objects back to [sync.Pool] upon Close.
type PooledResponse struct {
	Response
	fastReq  *fasthttp.Request
	fastResp *fasthttp.Response
	closed   atomic.Bool
}

// NewPooledResponse acquires a pooled [PooledResponse] adapter wrapping active fastReq and fastResp.
// Calling Close() thread-safely releases both fasthttp objects and recycles the adapter.
func NewPooledResponse(fastReq *fasthttp.Request, fastResp *fasthttp.Response) *PooledResponse {
	pr := pooledResponsePool.Get().(*PooledResponse)
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
