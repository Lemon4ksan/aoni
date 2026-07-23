// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"errors"
	"strconv"
	"strings"

	"github.com/lemon4ksan/aoni/internal/io"
)

var (
	// ErrResponseTooLarge indicates the response exceeded the size
	// limit configured via [option.WithMaxResponseSize].
	ErrResponseTooLarge = io.ErrResponseTooLarge

	// ErrBufferLimitExceeded indicates the replayable buffer exceeded its memory threshold,
	// and disk caching was disabled.
	ErrBufferLimitExceeded = io.ErrBufferLimitExceeded

	// ErrSSRFBlocked indicates the request was blocked because the
	// target resolved to a private or loopback address. Returned by
	// [Client.Request] when [option.WithSSRFGuard] is enabled.
	ErrSSRFBlocked = errors.New("aoni: request blocked by SSRF guard")

	// ErrCertificatePinning indicates the TLS handshake failed because
	// none of the peer certificates matched the configured public key pins.
	ErrCertificatePinning = errors.New("aoni: certificate pinning validation failed")

	// ErrNoCertificatesPresented is returned when a peer TLS handshake presents zero certificates.
	ErrNoCertificatesPresented = errors.New("aoni: no certificates presented by peer")

	// ErrInvalidPinFormat is returned when a certificate public key pin hash cannot be decoded.
	ErrInvalidPinFormat = errors.New("aoni: invalid pin format: must be 32-byte sha256 hash in base64 or hex")

	// ErrEmptyDNSProxy is returned by dialers if proxy dns is enabled but the proxy address is empty.
	ErrEmptyDNSProxy = errors.New("aoni: proxy DNS enabled but proxy address is empty")

	// ErrRedirectDomainForbidden is returned when a redirect domain is not allowed.
	ErrRedirectDomainForbidden = errors.New("aoni: redirect domain not allowed")
)

// Error represents a structured operation error in the aoni package.
type Error struct {
	Op     string
	Path   string
	Target string
	Err    error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	msg := "aoni: "
	if e.Op != "" {
		msg += e.Op + ": "
	}

	if e.Target != "" {
		msg += e.Target + ": "
	}

	if e.Path != "" {
		msg += e.Path + ": "
	}

	if e.Err != nil {
		msg += e.Err.Error()
	}

	return msg
}

func (e *Error) Unwrap() error { return e.Err }

// APIError wraps a non-2xx HTTP response. StatusCode holds the
// status code, Body holds the raw response, and Model holds the
// deserialized error structure when [WithErrorModel] was used.
// Inspect with [errors.As].
type APIError struct {
	StatusCode int
	Body       []byte
	Model      any
}

// Error returns a human-readable representation of e.
func (e *APIError) Error() string {
	statusStr := strconv.Itoa(e.StatusCode)
	if len(e.Body) > 0 {
		limit := min(len(e.Body), 128)
		cleanBody := strings.ReplaceAll(string(e.Body[:limit]), "\n", " ")
		return "aoni: status " + statusStr + " (body: " + cleanBody + ")"
	}

	return "aoni: status " + statusStr
}

// ValidationError reports that a required field was missing or
// invalid during request validation. Inspect with [errors.As] to
// access Field.
type ValidationError struct {
	Field string
}

// Error returns a human-readable description of the validation failure.
func (e *ValidationError) Error() string {
	return "aoni: missing required field: " + e.Field
}

// BridgeError represents an error occurring during standard-client bridging.
// It implements the standard error interface and can be unwrapped to retrieve
// the underlying client or transport errors.
type BridgeError struct {
	Op       string
	URL      string
	Err      error
	Metadata map[string]any
}

// Error implements the standard error interface.
func (e *BridgeError) Error() string {
	if e.Err != nil {
		return "aoni bridge: " + e.Op + " " + e.URL + ": " + e.Err.Error()
	}

	return "aoni bridge: " + e.Op + " " + e.URL
}

// Unwrap returns the underlying wrapped error.
func (e *BridgeError) Unwrap() error {
	return e.Err
}
