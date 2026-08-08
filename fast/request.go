// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fast

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"slices"
	"sync"

	"github.com/valyala/fasthttp"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/bytesconv"
)

var requestAdapterPool = sync.Pool{
	New: func() any { return &Request{} },
}

// Request adapts a high-performance [*fasthttp.Request] to the unified [aoni.Request] contract.
type Request struct {
	req        *fasthttp.Request
	ctx        context.Context
	getBody    func() (io.ReadCloser, error)
	isAcquired bool
}

// NewRequest acquires a pooled Request adapter wrapping req.
// The caller is responsible for releasing the request object.
func NewRequest(req *fasthttp.Request) *Request {
	isAcquired := false
	if req == nil {
		req = fasthttp.AcquireRequest()
		isAcquired = true
	}

	r := requestAdapterPool.Get().(*Request)
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
	f.req.SetRequestURI(urlStr)
}

// SetURIBytes assigns the destination address from a byte slice.
func (f *Request) SetURIBytes(uri []byte) {
	f.req.Header.SetRequestURIBytes(uri)
}

// Path yields the path component of the URL.
func (f *Request) Path() string {
	return bytesconv.B2S(f.req.URI().Path())
}

// SetPath assigns the path component of the URL.
func (f *Request) SetPath(path string) {
	f.req.URI().SetPath(path)
}

// RawQuery yields the raw query string.
func (f *Request) RawQuery() string {
	return bytesconv.B2S(f.req.URI().QueryArgs().QueryString())
}

// SetRawQuery assigns the raw query string.
func (f *Request) SetRawQuery(query string) {
	f.req.URI().SetQueryString(query)
}

// SetRawQueryBytes assigns the raw query string from a byte slice.
func (f *Request) SetRawQueryBytes(query []byte) {
	f.req.URI().SetQueryStringBytes(query)
}

// AddQueryParam appends a key-value query parameter to the URI.
func (f *Request) AddQueryParam(key, value string) {
	f.req.URI().QueryArgs().Add(key, value)
}

// AddQueryParamBytes appends a key-value query parameter using byte slices.
func (f *Request) AddQueryParamBytes(key, value []byte) {
	f.req.URI().QueryArgs().AddBytesKV(key, value)
}

// SetQueryParam sets or replaces a query parameter in the URI.
func (f *Request) SetQueryParam(key, value string) {
	f.req.URI().QueryArgs().Set(key, value)
}

// SetQueryParamBytes sets or replaces a query parameter using byte slices.
func (f *Request) SetQueryParamBytes(key, value []byte) {
	f.req.URI().QueryArgs().SetBytesKV(key, value)
}

// Header yields the header value for key as a string.
func (f *Request) Header(key string) string {
	return bytesconv.B2S(f.req.Header.Peek(key))
}

// HeaderBytes yields direct access to internal header buffer bytes.
func (f *Request) HeaderBytes(key []byte) []byte {
	return f.req.Header.PeekBytes(key)
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
	f.req.SetBody(body)
	f.getBody = nil
}

// BodyBytes yields direct access to internal fasthttp request body byte slice.
func (f *Request) BodyBytes() []byte {
	return f.req.Body()
}

// SetBodyStream assigns a streaming reader as request body and sets up rewind capabilities if supported.
func (f *Request) SetBodyStream(r io.Reader, contentLength int64) {
	f.req.SetBodyStream(r, int(contentLength))

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

// FastHTTPRequest yields the underlying [*fasthttp.Request] instance.
func (f *Request) FastHTTPRequest() *fasthttp.Request {
	return f.req
}

// EngineRequest yields the underlying [*fasthttp.Request] cast to any.
func (f *Request) EngineRequest() any {
	return f.req
}

// Release returns the Request adapter back to the pool.
func (f *Request) Release() {
	if f == nil {
		return
	}

	if f.isAcquired && f.req != nil {
		fasthttp.ReleaseRequest(f.req)
	}

	f.req = nil
	f.ctx = nil
	f.getBody = nil
	f.isAcquired = false
	requestAdapterPool.Put(f)
}

var _ aoni.Request = (*Request)(nil)
