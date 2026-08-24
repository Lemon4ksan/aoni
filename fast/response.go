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
	"unsafe"

	"github.com/lemon4ksan/foundation/codec/json"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/offheap"
	"github.com/lemon4ksan/foundation/silicon/pool"
	"golang.org/x/sys/cpu"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
	"github.com/lemon4ksan/aoni/internal/requestutil"
)

var (
	responseAdapterStorage = pool.NewPerPStorage(func() *Response {
		return &Response{}
	})
	pooledResponseStorage = pool.NewPerPStorage(func() *PooledResponse {
		return &PooledResponse{}
	})
)

type fastBodyReadCloser struct {
	io.Reader
	fastReq  *h1engine.Request
	fastResp *h1engine.Response
	once     sync.Once
}

func (b *fastBodyReadCloser) Bytes() (data []byte, volatile bool) {
	if b.fastResp != nil {
		return b.fastResp.Body(), true
	}

	return nil, false
}

func (b *fastBodyReadCloser) Close() error {
	b.once.Do(func() {
		h1engine.ReleaseRequest(b.fastReq)
		h1engine.ReleaseResponse(b.fastResp)
	})

	return nil
}

type bytesReadCloser struct {
	*bytes.Reader
	raw      []byte
	volatile bool
}

func (b *bytesReadCloser) Bytes() ([]byte, bool) {
	return b.raw, b.volatile
}

func (b *bytesReadCloser) Close() error {
	return nil
}

func newBytesReadCloser(data []byte, volatile bool) io.ReadCloser {
	return &bytesReadCloser{
		Reader:   bytes.NewReader(data),
		raw:      data,
		volatile: volatile,
	}
}

// Response adapts a high-performance [*h1engine.Response] to the unified [aoni.Response] contract.
//
// Memory Lifetime Invariants & Thread Safety:
// Response instances are recycled via [sync.Pool]. Callers MUST call [Response.Close] or [Response.Release]
// when finished processing the response to avoid socket leaks and memory fragmentation.
type Response struct {
	_            cpu.CacheLinePad
	resp         *h1engine.Response
	_            cpu.CacheLinePad
	trailers     map[string][]string
	uncompressed bool
	_            cpu.CacheLinePad
}

// NewResponse acquires a pooled [Response] adapter wrapping an active [*h1engine.Response].
// If resp is nil, a new [*h1engine.Response] is acquired automatically from [h1engine.AcquireResponse].
// Yields a zero-allocation adapter instance configured for pipeline processing.
func NewResponse(resp *h1engine.Response) *Response {
	if resp == nil {
		resp = h1engine.AcquireResponse()
	}

	r := responseAdapterStorage.Get()
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
		val = f.resp.Header.Peek(requestutil.CanonicalHeaderKey(key))
	}

	if len(val) == 0 {
		for k, v := range f.resp.Header.All() {
			if bytesconv.EqualFoldASCII(bytesconv.B2S(k), key) {
				val = v
				break
			}
		}
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
		val = f.resp.Header.PeekBytes(requestutil.CanonicalHeaderKeyBytes(key))
	}

	return val
}

// Headers yields all response headers as a canonical key-value map.
func (f *Response) Headers() map[string][]string {
	m := make(map[string][]string)
	for k, v := range f.resp.Header.All() {
		sk := requestutil.CanonicalHeaderKey(bytesconv.B2S(k))
		m[sk] = append(m[sk], string(v))
	}

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

// UnsafeAccess provides explicit, zero-allocation access to volatile response buffers.
type UnsafeAccess struct {
	resp *Response
}

// Bytes returns a direct slice of volatile response body memory without heap copying.
//
// Warning: Points into pooled memory. Do not retain beyond response lifetime.
func (u UnsafeAccess) Bytes() []byte {
	if u.resp == nil {
		return nil
	}

	return u.resp.UnsafeBodyBytes()
}

// String returns a zero-allocation string view over volatile response body memory.
func (u UnsafeAccess) String() string {
	return bytesconv.B2S(u.Bytes())
}

// Unsafe returns an [UnsafeAccess] accessor for explicit zero-copy operations.
func (f *Response) Unsafe() UnsafeAccess {
	return UnsafeAccess{resp: f}
}

// String returns a zero-allocation string view over volatile response body memory.
func (f *Response) String() string {
	if f == nil || f.resp == nil {
		return ""
	}

	return bytesconv.B2S(f.resp.Body())
}

// JSON decodes the response payload directly into v using the silicon-grade foundation/codec/json engine.
func (f *Response) JSON(v any) error {
	if f == nil || f.resp == nil {
		return io.EOF
	}

	body := f.resp.Body()
	if len(body) == 0 {
		return io.EOF
	}

	return json.Unmarshal(body, v)
}

// JSONNoCopy decodes the response payload into v referencing string fields directly without copying.
func (f *Response) JSONNoCopy(v any) error {
	if f == nil || f.resp == nil {
		return io.EOF
	}

	body := f.resp.Body()
	if len(body) == 0 {
		return io.EOF
	}

	return json.UnmarshalNoCopy(body, v)
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

	return newBytesReadCloser(f.UnsafeBodyBytes(), true)
}

// HTTPResponse converts fasthttp response adapter into standard *http.Response.
func (f *Response) HTTPResponse() *http.Response {
	if f == nil || f.resp == nil {
		return nil
	}

	header := make(http.Header)
	for k, v := range f.resp.Header.All() {
		header.Add(string(k), string(v))
	}

	body := slices.Clone(f.resp.Body())

	return &http.Response{
		StatusCode:    f.resp.StatusCode(),
		Status:        http.StatusText(f.resp.StatusCode()),
		Header:        header,
		Body:          newBytesReadCloser(body, false),
		ContentLength: int64(len(body)),
	}
}

// FastHTTPResponse yields the underlying [*h1engine.Response] instance.
func (f *Response) FastHTTPResponse() *h1engine.Response {
	return f.resp
}

// EngineResponse yields the underlying [*h1engine.Response] cast to any.
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

// WriteTo streams response body payload to w using off-heap kernel pages for zero mheap pressure.
func (f *Response) WriteTo(w io.Writer) (int64, error) {
	if f == nil || f.resp == nil {
		return 0, nil
	}

	body := f.resp.Body()
	if len(body) > 0 {
		n, err := w.Write(body)
		return int64(n), err
	}

	if !f.resp.IsBodyStream() {
		return 0, nil
	}

	stream := f.resp.BodyStream()
	if stream == nil {
		return 0, nil
	}

	var (
		total     int64
		streamErr error
	)

	_ = offheap.Scope(64*1024, func(arena *offheap.Arena) {
		ptr := arena.Alloc(64 * 1024)
		if ptr == nil {
			total, streamErr = io.Copy(w, stream)
			return
		}

		tmp := unsafe.Slice((*byte)(ptr), 64*1024)

		for {
			nr, rErr := stream.Read(tmp)
			if nr > 0 {
				nw, wErr := w.Write(tmp[:nr])
				total += int64(nw)

				if wErr != nil {
					streamErr = wErr
					return
				}
			}

			if rErr == io.EOF {
				return
			}

			if rErr != nil {
				streamErr = rErr
				return
			}
		}
	})

	return total, streamErr
}

// Release returns the Response adapter instance back to [sync.Pool] for memory recycling.
func (f *Response) Release() {
	if f == nil {
		return
	}

	f.resp = nil
	f.trailers = nil
	f.uncompressed = false
	responseAdapterStorage.Put(f)
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
	_ cpu.CacheLinePad
	Response
	_        cpu.CacheLinePad
	fastReq  *h1engine.Request
	fastResp *h1engine.Response
	closed   atomic.Bool
	_        cpu.CacheLinePad
}

// NewPooledResponse acquires a pooled [PooledResponse] adapter wrapping active fastReq and fastResp.
// Calling Close() thread-safely releases both fasthttp objects and recycles the adapter.
func NewPooledResponse(fastReq *h1engine.Request, fastResp *h1engine.Response) *PooledResponse {
	pr := pooledResponseStorage.Get()
	pr.resp = fastResp
	pr.trailers = nil
	pr.uncompressed = false
	pr.fastReq = fastReq
	pr.fastResp = fastResp
	pr.closed.Store(false)

	return pr
}

// HTTPResponse converts PooledResponse into standard *http.Response.
func (r *PooledResponse) HTTPResponse() *http.Response {
	if r == nil || r.fastResp == nil {
		return nil
	}

	header := make(http.Header)
	for k, v := range r.fastResp.Header.All() {
		header.Add(string(k), string(v))
	}

	body := slices.Clone(r.fastResp.Body())

	return &http.Response{
		StatusCode:    r.fastResp.StatusCode(),
		Status:        http.StatusText(r.fastResp.StatusCode()),
		Header:        header,
		Body:          newBytesReadCloser(body, false),
		ContentLength: int64(len(body)),
	}
}

// Close releases underlying fasthttp objects and returns PooledResponse to memory pool.
func (r *PooledResponse) Close() error {
	if !r.closed.Swap(true) {
		if r.fastReq != nil {
			h1engine.ReleaseRequest(r.fastReq)
			r.fastReq = nil
		}

		if r.fastResp != nil {
			h1engine.ReleaseResponse(r.fastResp)
			r.fastResp = nil
		}

		r.resp = nil
		r.trailers = nil
		r.uncompressed = false
		pooledResponseStorage.Put(r)
	}

	return nil
}

var (
	_ aoni.Response = (*Response)(nil)
	_ aoni.Response = (*PooledResponse)(nil)
)
