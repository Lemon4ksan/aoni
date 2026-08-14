// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"errors"
	"strconv"
	"strings"

	"github.com/lemon4ksan/aoni/internal/bytesconv"
	"github.com/lemon4ksan/aoni/internal/io"
	"github.com/lemon4ksan/aoni/internal/pipeline"
)

var (
	// ErrNilRequest is returned when an operation is executed on a nil Request contract.
	ErrNilRequest = errors.New("aoni: request is nil")

	// ErrInvalidPath indicates that the provided URL path could not be parsed.
	ErrInvalidPath = errors.New("aoni: invalid path")

	// ErrMaxRedirectsExceeded is returned when the request execution halts because the maximum redirect threshold was reached.
	ErrMaxRedirectsExceeded = errors.New("aoni: maximum redirects limit exceeded")

	// ErrResponseTooLarge indicates that response payload length exceeded configured bounds.
	ErrResponseTooLarge = io.ErrResponseTooLarge

	// ErrBufferLimitExceeded indicates replayable buffer size exceeded memory threshold without disk backing.
	ErrBufferLimitExceeded = io.ErrBufferLimitExceeded

	// ErrRedirectDomainForbidden is returned when a redirect target hostname is excluded by policy.
	ErrRedirectDomainForbidden = errors.New("aoni: redirect domain not allowed")

	// ErrHedgingBodyNonRepeatable is returned when request hedging attempt cannot duplicate a non-replayable payload stream.
	ErrHedgingBodyNonRepeatable = errors.New("aoni: request body cannot be duplicated for hedging")

	// ErrConflictingContentLength is returned when a response carries multiple conflicting Content-Length headers (RFC 9112).
	ErrConflictingContentLength = pipeline.ErrConflictingContentLength

	// ErrConflictingLocationHeader is returned when a response carries multiple conflicting Location headers.
	ErrConflictingLocationHeader = pipeline.ErrConflictingLocationHeader

	// ErrHeaderInjectionDetected is returned when a response header contains CRLF control characters.
	ErrHeaderInjectionDetected = pipeline.ErrHeaderInjectionDetected
)

// Error describes a structured operational failure in the aoni package.
type Error struct {
	// Err holds the underlying cause error.
	Err error
	// Op identifies the high-level operation during which the failure occurred.
	Op string
	// Path specifies the request path or URI associated with the error.
	Path string
	// Target identifies the remote target host or address.
	Target string
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	var sb strings.Builder
	sb.Grow(len(e.Op) + len(e.Target) + len(e.Path) + 32)
	sb.WriteString("aoni: ")

	if e.Op != "" {
		sb.WriteString(e.Op)
		sb.WriteString(": ")
	}

	if e.Target != "" {
		sb.WriteString(e.Target)
		sb.WriteString(": ")
	}

	if e.Path != "" {
		sb.WriteString(e.Path)
		sb.WriteString(": ")
	}

	if e.Err != nil {
		sb.WriteString(e.Err.Error())
	}

	return sb.String()
}

func (e *Error) Unwrap() error { return e.Err }

// APIError describes an HTTP response status failure (>= 400).
type APIError struct {
	// Model holds an unmarshaled error envelope structure if parsed by a decoder.
	Model any
	// Body contains a snippet or full payload of the HTTP response error body.
	Body []byte
	// StatusCode records the non-2xx HTTP response status code (e.g., 400, 404, 500).
	StatusCode int
}

func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}

	var numBuf [10]byte

	statusBytes := strconv.AppendInt(numBuf[:0], int64(e.StatusCode), 10)

	if len(e.Body) == 0 {
		return "aoni: status " + bytesconv.B2S(statusBytes)
	}

	limit := min(len(e.Body), 128)
	bodySlice := e.Body[:limit]

	cleanBody := bytes.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return ' '
		}

		return r
	}, bodySlice)

	var sb strings.Builder
	sb.Grow(32 + len(cleanBody))
	sb.WriteString("aoni: status ")
	sb.Write(statusBytes)
	sb.WriteString(" (body: ")
	sb.Write(cleanBody)
	sb.WriteByte(')')

	return sb.String()
}

// BridgeError describes an execution failure during stdlib [http.Client] bridging.
type BridgeError struct {
	// Err holds the underlying transport or pipeline error.
	Err error
	// Metadata contains additional context metadata (e.g. host, scheme).
	Metadata map[string]any
	// Op specifies the HTTP request method during which the error occurred.
	Op string
	// URL specifies the target request URL string.
	URL string
}

func (e *BridgeError) Error() string {
	if e == nil {
		return "<nil>"
	}

	var sb strings.Builder
	sb.Grow(len(e.Op) + len(e.URL) + 32)
	sb.WriteString("aoni/bridge: ")
	sb.WriteString(e.Op)
	sb.WriteByte(' ')
	sb.WriteString(e.URL)

	if e.Err != nil {
		sb.WriteString(": ")
		sb.WriteString(e.Err.Error())
	}

	return sb.String()
}

func (e *BridgeError) Unwrap() error {
	return e.Err
}
