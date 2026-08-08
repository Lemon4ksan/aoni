// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
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
	Err    error
	Op     string
	Path   string
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
	Model      any
	Body       []byte
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

	var sb strings.Builder
	sb.Grow(32 + len(bodySlice))
	sb.WriteString("aoni: status ")
	sb.Write(statusBytes)
	sb.WriteString(" (body: ")

	for _, b := range bodySlice {
		if b == '\n' || b == '\r' {
			sb.WriteByte(' ')
		} else {
			sb.WriteByte(b)
		}
	}

	sb.WriteByte(')')

	return sb.String()
}

// ValidationError reports that a required field failed schema validation.
type ValidationError struct {
	Field string
}

func (e *ValidationError) Error() string {
	return "aoni: missing required field: " + e.Field
}

// BridgeError describes an execution failure during stdlib [http.Client] bridging.
type BridgeError struct {
	Err      error
	Metadata map[string]any
	Op       string
	URL      string
}

func (e *BridgeError) Error() string {
	if e == nil {
		return "<nil>"
	}

	var sb strings.Builder
	sb.Grow(len(e.Op) + len(e.URL) + 32)
	sb.WriteString("aoni bridge: ")
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
