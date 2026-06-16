// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"errors"
	"fmt"
)

// APIError represents an unsuccessful HTTP response (status code outside 2xx).
// It captures the raw response body, which often contains error details from the server.
type APIError struct {
	// StatusCode is the HTTP status code returned by the server.
	StatusCode int
	// Body is the raw response body.
	Body []byte
	// Model is the deserialized error structure, if any.
	Model any
}

func (e *APIError) Error() string {
	return fmt.Sprintf("aoni: status %d", e.StatusCode)
}

// ValidationError is returned when a request structure fails validation.
type ValidationError struct {
	// Field is the name of the missing or invalid struct field.
	Field string
}

func (e *ValidationError) Error() string {
	return "aoni: missing required field: " + e.Field
}

var (
	// ErrUnexpectedContentType is returned when the response content type does not match the expected format (e.g. got HTML when expecting JSON).
	ErrUnexpectedContentType = errors.New("aoni: unexpected content-type (possible captive portal or intercept)")
	// ErrCloudflareChallenge is returned when a Cloudflare JS challenge or CAPTCHA is detected in the response body.
	ErrCloudflareChallenge = errors.New("aoni: cloudflare challenge detected")
	// ErrResponseTooLarge is returned when the response size exceeds the configured maximum limit.
	ErrResponseTooLarge = errors.New("aoni: response size limit exceeded")
	// ErrSSRFBlocked is returned when a request is blocked because it resolved to a private/local IP address.
	ErrSSRFBlocked = errors.New("aoni: request blocked by SSRF guard")
)
