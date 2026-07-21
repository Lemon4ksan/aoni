// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"errors"
	"fmt"

	"github.com/lemon4ksan/aoni/internal/io"
)

var (
	// ErrUnexpectedContentType indicates the response content type
	// does not match the expected format. A captive portal or
	// transparent proxy often causes this.
	ErrUnexpectedContentType = errors.New("aoni: unexpected content-type (possible captive portal or intercept)")

	// ErrCloudflareChallenge indicates a Cloudflare JS challenge or
	// CAPTCHA was detected in the response body.
	ErrCloudflareChallenge = errors.New("aoni: cloudflare challenge detected")

	// ErrChallengeRequired indicates that a WAF challenge (Cloudflare, CAPTCHA, etc.)
	// has been detected and solver verification is required.
	ErrChallengeRequired = errors.New("aoni: challenge verification required")

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

	// ErrEmptyDNSProxy is returned by dialers if proxy dns is enabled but the proxy adress is empty.
	ErrEmptyDNSProxy = errors.New("aoni: proxy DNS enabled but proxy address is empty")
)

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
	return fmt.Sprintf("aoni: status %d", e.StatusCode)
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
	return fmt.Sprintf("aoni bridge: %s %s: %v", e.Op, e.URL, e.Err)
}

// Unwrap returns the underlying wrapped error.
func (e *BridgeError) Unwrap() error {
	return e.Err
}
