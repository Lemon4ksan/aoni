// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"

	"github.com/lemon4ksan/aoni/internal/std"
	"github.com/lemon4ksan/aoni/mod"
)

// StdRequest adapts a standard net/http [*http.Request] to the unified [Request] contract.
type StdRequest = std.Request

// StdResponse adapts a standard net/http [*http.Response] to the unified [Response] contract.
type StdResponse = std.Response

// NewStdRequest wraps a standard *http.Request into a unified [Request] adapter.
func NewStdRequest(req *http.Request) *StdRequest {
	return std.NewRequest(req)
}

// NewStdResponse wraps a standard *http.Response into a unified [Response] adapter.
func NewStdResponse(resp *http.Response) *StdResponse {
	return std.NewResponse(resp)
}

// HTTPDoer specifies the minimal execution contract for processing standard *http.Request transactions.
// It matches the exact signature of standard library [*http.Client.Do].
type HTTPDoer = std.HTTPDoer

// HTTPDoerFunc adapts a plain execution closure to the [HTTPDoer] interface.
type HTTPDoerFunc = std.HTTPDoerFunc

// NewHTTPDoerAdapter wraps doer in a [RequestDoer] adapter. Safe for concurrent execution.
func NewHTTPDoerAdapter(doer HTTPDoer) RequestDoer {
	return std.NewHTTPDoerAdapter(doer)
}

// NewRequestDoerAdapter wraps doer in an [HTTPDoer] adapter. Safe for concurrent execution.
func NewRequestDoerAdapter(doer RequestDoer) HTTPDoer {
	return std.NewRequestDoerAdapter(doer)
}

// ToStdRequest converts a generic [Request] interface into a standard [*http.Request].
func ToStdRequest(req Request) (*http.Request, error) {
	return std.ToHTTPRequest(req)
}

type requesterLike interface {
	Request(ctx context.Context, method, path string, mods ...RequestModifier) (*http.Response, error)
}

type requesterHTTPDoer struct {
	r requesterLike
}

func (d requesterHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, ErrNilURL
	}

	mods := make([]RequestModifier, 0, len(req.Header)+1)
	for k, vv := range req.Header {
		for _, v := range vv {
			mods = append(mods, mod.WithHeader(k, v))
		}
	}

	if req.Body != nil && req.Body != http.NoBody {
		mods = append(mods, mod.WithSmartBody(req.Body))
	}

	return d.r.Request(req.Context(), req.Method, req.URL.String(), mods...)
}

func (d requesterHTTPDoer) Unwrap() any {
	return d.r
}
