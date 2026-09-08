// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import "net/http"

// BuilderPlugin configures or enhances a [RequestBuilder] instance.
type BuilderPlugin interface {
	ApplyBuilder(r *RequestBuilder) error
}

// BuilderPluginFunc adapts a function to satisfy the [BuilderPlugin] interface.
type BuilderPluginFunc func(r *RequestBuilder) error

// ApplyBuilder executes the underlying function against r.
func (f BuilderPluginFunc) ApplyBuilder(r *RequestBuilder) error {
	return f(r)
}

// RequestSigner applies late-binding signatures (e.g. AWS SigV4, HMAC) to an outgoing *http.Request.
type RequestSigner interface {
	SignRequest(req *http.Request) error
}

// RequestSignerFunc adapts a function to satisfy the [RequestSigner] interface.
type RequestSignerFunc func(req *http.Request) error

// SignRequest executes the underlying signature function against req.
func (f RequestSignerFunc) SignRequest(req *http.Request) error {
	return f(req)
}

// ResponseSink defines a pluggable consumer for HTTP response payloads (e.g. files, streams, custom decoders).
type ResponseSink interface {
	ConsumeResponse(resp *http.Response) error
}

// ResponseValidator validates an HTTP response before payload decoding begins.
type ResponseValidator interface {
	ValidateResponse(resp *http.Response) error
}

// ResponseValidatorFunc adapts a function to satisfy the [ResponseValidator] interface.
type ResponseValidatorFunc func(resp *http.Response) error

// ValidateResponse executes the underlying validation function against resp.
func (f ResponseValidatorFunc) ValidateResponse(resp *http.Response) error {
	return f(resp)
}
