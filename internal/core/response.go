// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import (
	"io"
	"net/http"
)

// Response defines the unified, engine-agnostic HTTP response contract.
// It manages status codes, response headers, HTTP/2 & HTTP/3 trailers, and body streams.
type Response interface {
	StatusCode() int
	Status() string
	StatusBytes() []byte

	Header(key string) string
	HeaderBytes(key []byte) []byte
	Headers() map[string][]string

	Trailers() map[string][]string
	SetTrailers(trailers map[string][]string)

	BodyBytes() []byte
	UnsafeBodyBytes() []byte
	BodyStream() io.ReadCloser

	HTTPResponse() *http.Response
	EngineResponse() any

	Uncompressed() bool
	SetUncompressed(v bool)
	Close() error
}

// ResponseDecoder defines the contract for unmarshaling response payload streams into Go structures.
type ResponseDecoder interface {
	Decode(reader io.Reader, target any) error
}

// BaseResponse is the interface implemented by structured envelope responses
// (e.g., API response wrappers containing { "data": ..., "error": ... }).
type BaseResponse interface {
	IsSuccess() bool
	Error() error
	SetData(data any)
}

// BaseResponseProvider provides a [BaseResponse] model factory for structured envelope unwrapping.
type BaseResponseProvider interface {
	// BaseResponse constructs or returns a zero-value BaseResponse envelope instance.
	BaseResponse() BaseResponse
}
