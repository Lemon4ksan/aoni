// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package std

import (
	"bytes"
	"context"
	"errors"
	stdio "io"
	"net/http"
	"strconv"

	"github.com/lemon4ksan/foundation/silicon/bytesconv"

	"github.com/lemon4ksan/aoni/internal/core"
)

// ErrNilRequest is returned when attempting to execute a nil request.
var ErrNilRequest = errors.New("aoni/std: request is nil")

// HTTPDoer specifies the minimal execution contract for processing standard *http.Request transactions.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// HTTPDoerFunc adapts a plain execution closure to the [HTTPDoer] interface.
type HTTPDoerFunc func(req *http.Request) (*http.Response, error)

// Do executes the underlying closure against the provided HTTP request.
func (f HTTPDoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// HTTPDoerAdapter adapts an [HTTPDoer] to the unified [core.RequestDoer] interface.
type HTTPDoerAdapter struct {
	doer HTTPDoer
}

// NewHTTPDoerAdapter wraps doer in a [core.RequestDoer] adapter. Safe for concurrent execution.
func NewHTTPDoerAdapter(doer HTTPDoer) core.RequestDoer {
	if doer == nil {
		return nil
	}

	return &HTTPDoerAdapter{doer: doer}
}

// Do executes a unified [core.Request] via the underlying [HTTPDoer]. Safe for concurrent execution.
func (a *HTTPDoerAdapter) Do(req core.Request) (core.Response, error) {
	if a == nil || a.doer == nil {
		return nil, ErrNilRequest
	}

	httpReq := req.HTTPRequest()
	if httpReq == nil {
		return nil, ErrNilRequest
	}

	resp, err := a.doer.Do(httpReq) //nolint:bodyclose
	if err != nil {
		return nil, err
	}

	return NewResponse(resp), nil
}

// ResponseBodyCloser decorates an [stdio.ReadCloser] stream and ensures the parent [core.Response] is released upon body close.
type ResponseBodyCloser struct {
	stdio.ReadCloser
	Resp core.Response
}

func (r *ResponseBodyCloser) Close() error {
	var err error
	if r.ReadCloser != nil {
		err = r.ReadCloser.Close()
	}

	if r.Resp != nil {
		_ = r.Resp.Close()
	}

	return err
}

// RequestDoerAdapter adapts a [core.RequestDoer] to the legacy [HTTPDoer] interface.
type RequestDoerAdapter struct {
	doer core.RequestDoer
}

// NewRequestDoerAdapter wraps doer in an [HTTPDoer] adapter. Safe for concurrent execution.
func NewRequestDoerAdapter(doer core.RequestDoer) HTTPDoer {
	if doer == nil {
		return nil
	}

	return &RequestDoerAdapter{doer: doer}
}

// Do executes a standard [*http.Request] via the underlying [spec.RequestDoer]. Safe for concurrent execution.
func (a *RequestDoerAdapter) Do(req *http.Request) (*http.Response, error) {
	if a == nil || a.doer == nil {
		return nil, ErrNilRequest
	}

	stdReq := NewRequest(req)

	resp, err := a.doer.Do(stdReq)
	if err != nil {
		return nil, err
	}

	if stdResp, ok := resp.(*Response); ok {
		return stdResp.resp, nil
	}

	if httpResp := resp.HTTPResponse(); httpResp != nil {
		if httpResp.Trailer == nil && len(resp.Trailers()) > 0 {
			httpResp.Trailer = make(http.Header)
			for k, vv := range resp.Trailers() {
				for _, v := range vv {
					httpResp.Trailer.Add(k, v)
				}
			}
		}

		return httpResp, nil
	}

	httpResp := &http.Response{
		StatusCode:    resp.StatusCode(),
		Status:        resp.Status(),
		Header:        make(http.Header),
		Trailer:       make(http.Header),
		Body:          &ResponseBodyCloser{ReadCloser: resp.BodyStream(), Resp: resp},
		ContentLength: -1,
		Uncompressed:  resp.Uncompressed(),
		Request:       req,
	}

	for k, vv := range resp.Headers() {
		for _, v := range vv {
			httpResp.Header.Add(k, v)
		}
	}

	for k, vv := range resp.Trailers() {
		for _, v := range vv {
			httpResp.Trailer.Add(k, v)
		}
	}

	if clStr := resp.Header("Content-Length"); clStr != "" {
		if cl, parseErr := strconv.ParseInt(clStr, 10, 64); parseErr == nil {
			httpResp.ContentLength = cl
		}
	}

	return httpResp, nil
}

// ToHTTPRequest converts a generic [core.Request] interface into a standard [*http.Request].
func ToHTTPRequest(req core.Request) (*http.Request, error) {
	if httpReq := req.HTTPRequest(); httpReq != nil {
		return httpReq, nil
	}

	ctx := req.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	body := req.BodyStream()
	if body == nil {
		if bb := req.BodyBytes(); len(bb) > 0 {
			body = bytes.NewReader(bb)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method(), req.URL(), body)
	if err != nil {
		return nil, err
	}

	for k, v := range req.Headers() {
		httpReq.Header.Add(bytesconv.B2S(k), bytesconv.B2S(v))
	}

	if host := req.Header("Host"); host != "" {
		httpReq.Host = host
	}

	return httpReq, nil
}
