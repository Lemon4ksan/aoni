// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"errors"
	"fmt"
)

var (
	// ErrUnexpectedContentType indicates the response content type
	// does not match the expected format. A captive portal or
	// transparent proxy often causes this.
	ErrUnexpectedContentType = errors.New("aoni: unexpected content-type (possible captive portal or intercept)")

	// ErrCloudflareChallenge indicates a Cloudflare JS challenge or
	// CAPTCHA was detected in the response body.
	ErrCloudflareChallenge = errors.New("aoni: cloudflare challenge detected")

	// ErrResponseTooLarge indicates the response exceeded the size
	// limit configured via [Client.WithMaxResponseSize].
	ErrResponseTooLarge = errors.New("aoni: response size limit exceeded")

	// ErrSSRFBlocked indicates the request was blocked because the
	// target resolved to a private or loopback address. Returned by
	// [Client.Request] when [Client.WithSSRFGuard] is enabled.
	ErrSSRFBlocked = errors.New("aoni: request blocked by SSRF guard")
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
