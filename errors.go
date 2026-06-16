// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"errors"
	"fmt"
)

var (
	// ErrUnexpectedContentType is returned when the response content type does not match the expected format.
	// This often indicates a captive portal or unexpected network interception.
	ErrUnexpectedContentType = errors.New("aoni: unexpected content-type (possible captive portal or intercept)")

	// ErrCloudflareChallenge is returned when a Cloudflare JS challenge or CAPTCHA is detected in the response body.
	ErrCloudflareChallenge = errors.New("aoni: cloudflare challenge detected")

	// ErrResponseTooLarge is returned when the response size exceeds the configured maximum limit.
	// It is returned by [Client.Request] if response size checking is active.
	ErrResponseTooLarge = errors.New("aoni: response size limit exceeded")

	// ErrSSRFBlocked is returned when a request is blocked because it resolved to a private or local IP address.
	// It is returned by [Client.Request] if SSRF protection is enabled.
	ErrSSRFBlocked = errors.New("aoni: request blocked by SSRF guard")
)

// APIError represents an unsuccessful HTTP response with a status code outside the 2xx range.
// It contains the raw response body and a deserialized error model if a custom error model
// was configured on the [Client]. Inspect this error using [errors.As] to handle API failures.
type APIError struct {
	// StatusCode is the HTTP status code returned by the server.
	StatusCode int
	// Body is the raw response body.
	Body []byte
	// Model is the deserialized error structure, if any.
	Model any
}

// Error returns a formatted string representation of the [APIError].
func (e *APIError) Error() string {
	return fmt.Sprintf("aoni: status %d", e.StatusCode)
}

// ValidationError is returned when a request structure fails validation.
// Inspect this error using [errors.As] to determine which field triggered the validation failure.
type ValidationError struct {
	// Field is the name of the missing or invalid struct field.
	Field string
}

func (e *ValidationError) Error() string {
	return "aoni: missing required field: " + e.Field
}
