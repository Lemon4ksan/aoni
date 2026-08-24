// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"bytes"
	"context"
	"io"
	"iter"
	"net/http"
	"slices"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"
	"github.com/lemon4ksan/foundation/silicon/pool"
	"golang.org/x/sys/cpu"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/fast/h1engine"
)

var requestAdapterStorage = pool.NewPerPStorage(func() *Request {
	return &Request{}
})

// Request adapts a high-performance [*h1engine.Request] to the unified [aoni.Request] contract.
//
// Thread Safety & Memory Lifetime Invariants:
// Request instances are recycled via sharded [pool.PerPStorage] for zero-allocation, zero-lock execution.
// Callers acquiring requests via [NewRequest] or [Client.AcquireRequest] MUST release them
// via [Client.ReleaseRequest] or [Request.Release] when request lifecycle terminates.
type Request struct {
	_          cpu.CacheLinePad
	req        *h1engine.Request
	_          cpu.CacheLinePad
	ctx        context.Context
	getBody    func() (io.ReadCloser, error)
	isAcquired bool
	_          cpu.CacheLinePad
}

// NewRequest acquires a pooled [Request] adapter wrapping an active [*h1engine.Request].
// If req is nil, a new [*h1engine.Request] is acquired automatically from [h1engine.AcquireRequest].
// Yields a ready-to-use [Request] adapter bound to the pool. Caller MUST call Release() when finished.
func NewRequest(req *h1engine.Request) *Request {
	isAcquired := false
	if req == nil {
		req = h1engine.AcquireRequest()
		req.Reset()

		isAcquired = true
	}

	r := requestAdapterStorage.Get()
	r.req = req
	r.ctx = nil
	r.getBody = nil
	r.isAcquired = isAcquired

	return r
}

// Context yields the execution context, defaulting to context.Background.
func (f *Request) Context() context.Context {
	if f.ctx == nil {
		return context.Background()
	}

	return f.ctx
}

// SetContext assigns the execution context to the request adapter.
func (f *Request) SetContext(ctx context.Context) {
	f.ctx = ctx
}

// Method yields the HTTP method string.
func (f *Request) Method() string {
	return bytesconv.B2S(f.req.Header.Method())
}

// SetMethod assigns the HTTP method string.
func (f *Request) SetMethod(method string) {
	f.req.Header.SetMethod(method)
}

// SetMethodBytes assigns the HTTP method from a byte slice without allocations.
func (f *Request) SetMethodBytes(method []byte) {
	f.req.Header.SetMethodBytes(method)
}

// URL yields the full target URL string.
func (f *Request) URL() string {
	return bytesconv.B2S(f.req.URI().FullURI())
}

// SetURL assigns the destination address string.
func (f *Request) SetURL(urlStr string) {
	var methodBuf [16]byte

	rawMethod := f.req.Header.Method()
	n := copy(methodBuf[:], rawMethod)

	f.req.SetRequestURI(urlStr)

	if n > 0 {
		f.req.Header.SetMethodBytes(methodBuf[:n])
	}

	if host := f.req.URI().Host(); len(host) > 0 {
		f.req.Header.SetHostBytes(host)
	}
}

// SetURIBytes assigns the destination address from a byte slice.
func (f *Request) SetURIBytes(uri []byte) {
	f.req.Header.SetRequestURIBytes(uri)
}

// Path yields the path component of the URL.
func (f *Request) Path() string {
	return bytesconv.B2S(f.req.URI().Path())
}

// SetPath sets the request URI path component.
func (f *Request) SetPath(path string) {
	f.req.URI().SetPath(path)
	f.req.Header.SetRequestURIBytes(f.req.URI().RequestURI())
}

// SetPathBytes sets the request URI path component from a byte slice.
func (f *Request) SetPathBytes(path []byte) {
	f.req.URI().SetPathBytes(path)
	f.req.Header.SetRequestURIBytes(f.req.URI().RequestURI())
}

// RawQuery yields the raw query string.
func (f *Request) RawQuery() string {
	return bytesconv.B2S(f.req.URI().QueryArgs().QueryString())
}

// SetRawQuery assigns the raw query string.
func (f *Request) SetRawQuery(query string) {
	f.req.URI().SetQueryString(query)
	f.req.Header.SetRequestURIBytes(f.req.URI().RequestURI())
}

// SetRawQueryBytes assigns the raw query string from a byte slice.
func (f *Request) SetRawQueryBytes(query []byte) {
	f.req.URI().SetQueryStringBytes(query)
	f.req.Header.SetRequestURIBytes(f.req.URI().RequestURI())
}

// AddQueryParam appends a key-value query parameter to the URI.
func (f *Request) AddQueryParam(key, value string) {
	f.req.URI().QueryArgs().Add(key, value)
	f.req.Header.SetRequestURIBytes(f.req.URI().RequestURI())
}

// AddQueryParamBytes appends a key-value query parameter using byte slices.
func (f *Request) AddQueryParamBytes(key, value []byte) {
	f.req.URI().QueryArgs().AddBytesKV(key, value)
	f.req.Header.SetRequestURIBytes(f.req.URI().RequestURI())
}

// SetQueryParam sets or replaces a query parameter in the URI.
func (f *Request) SetQueryParam(key, value string) {
	f.req.URI().QueryArgs().Set(key, value)
	f.req.Header.SetRequestURIBytes(f.req.URI().RequestURI())
}

// SetQueryParamBytes sets or replaces a query parameter using byte slices.
func (f *Request) SetQueryParamBytes(key, value []byte) {
	f.req.URI().QueryArgs().SetBytesKV(key, value)
	f.req.Header.SetRequestURIBytes(f.req.URI().RequestURI())
}

// Header yields the header value for key as a string.
func (f *Request) Header(key string) string {
	return bytesconv.B2S(f.req.Header.Peek(key))
}

// HeaderBytes yields direct access to internal header buffer bytes.
func (f *Request) HeaderBytes(key []byte) []byte {
	return f.req.Header.PeekBytes(key)
}

// Headers yields all header key-value pairs as an iterator.
func (f *Request) Headers() iter.Seq2[[]byte, []byte] {
	return f.req.Header.All()
}

// SetHeader sets header key to value.
func (f *Request) SetHeader(key, value string) {
	f.req.Header.Set(key, value)
}

// SetHeaderBytes sets header key to value using byte slices.
func (f *Request) SetHeaderBytes(key, value []byte) {
	f.req.Header.SetBytesKV(key, value)
}

// AddHeader appends value to header key.
func (f *Request) AddHeader(key, value string) {
	f.req.Header.Add(key, value)
}

// AddHeaderBytes appends value to header key using byte slices.
func (f *Request) AddHeaderBytes(key, value []byte) {
	f.req.Header.AddBytesKV(key, value)
}

// DelHeader removes header key.
func (f *Request) DelHeader(key string) {
	f.req.Header.Del(key)
}

// DelHeaderBytes removes header key using a byte slice.
func (f *Request) DelHeaderBytes(key []byte) {
	f.req.Header.DelBytes(key)
}

// ResetHeaders removes all headers from the request.
func (f *Request) ResetHeaders() {
	f.req.Header.Reset()
}

// SetBodyBytes sets request body to a raw byte slice.
func (f *Request) SetBodyBytes(body []byte) {
	existingCT := f.req.Header.ContentType()
	f.req.SetBody(body)

	if len(existingCT) > 0 {
		f.req.Header.SetContentTypeBytes(existingCT)
	}

	f.getBody = nil
}

// BodyBytes yields direct access to internal fasthttp request body byte slice.
func (f *Request) BodyBytes() []byte {
	return f.req.Body()
}

// SetBodyStream assigns a streaming reader as request body and sets up rewind capabilities if supported.
func (f *Request) SetBodyStream(r io.Reader, contentLength int64) {
	existingCT := f.req.Header.ContentType()
	f.req.SetBodyStream(r, int(contentLength))

	if len(existingCT) > 0 {
		f.req.Header.SetContentTypeBytes(existingCT)
	}

	if rc, ok := r.(io.ReadCloser); ok {
		f.getBody = func() (io.ReadCloser, error) {
			return rc, nil
		}
	} else if seeker, ok := r.(io.Seeker); ok {
		f.getBody = func() (io.ReadCloser, error) {
			if _, err := seeker.Seek(0, io.SeekStart); err != nil {
				return nil, err
			}

			if rc, ok := r.(io.ReadCloser); ok {
				return rc, nil
			}

			return io.NopCloser(r), nil
		}
	} else {
		f.getBody = nil
	}
}

// SetGetBody assigns a custom generator for rewinding body streams during retries and 307/308 redirects.
func (f *Request) SetGetBody(fn func() (io.ReadCloser, error)) {
	f.getBody = fn
}

// GetBody generates a fresh ReadCloser for replaying the request payload stream.
func (f *Request) GetBody() (io.ReadCloser, error) {
	if f.getBody != nil {
		return f.getBody()
	}

	if body := f.req.Body(); len(body) > 0 {
		return io.NopCloser(bytes.NewReader(slices.Clone(body))), nil
	}

	return nil, nil
}

// BodyStream yields an io.Reader for the request body.
func (f *Request) BodyStream() io.Reader {
	return f.req.BodyStream()
}

// HTTPRequest yields nil for fasthttp request adapters.
func (f *Request) HTTPRequest() *http.Request {
	return nil
}

// FastHTTPRequest yields the underlying [*h1engine.Request] instance.
func (f *Request) FastHTTPRequest() *h1engine.Request {
	return f.req
}

// EngineRequest yields the underlying [*h1engine.Request] cast to any.
func (f *Request) EngineRequest() any {
	return f.req
}

// Release returns the Request adapter back to the pool.
func (f *Request) Release() {
	if f == nil {
		return
	}

	f.req = nil
	f.ctx = nil
	f.getBody = nil
	f.isAcquired = false
	requestAdapterStorage.Put(f)
}

var _ aoni.Request = (*Request)(nil)
