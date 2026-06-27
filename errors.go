// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"errors"
	"fmt"
	"net"
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

// DNSError represents an error occurring during DNS resolution in the aoni package.
// It implements the standard error interface and can be unwrapped to retrieve
// the underlying network or protocol error.
type DNSError struct {
	// Host is the domain name that was queried for resolution.
	Host string
	// Resolver is the type of the resolver that failed (e.g., "DoH", "DoT", "InMemoryCache").
	Resolver string
	// Endpoint is the network address of the DNS server queried, if applicable.
	Endpoint string
	// Err is the underlying cause of the DNS resolution failure.
	Err error
	// IsTimeout indicates whether the failure was caused by a network timeout.
	IsTimeout bool
}

// Error implements the standard error interface.
func (e *DNSError) Error() string {
	if e.Endpoint != "" {
		return fmt.Sprintf("aoni dns: resolve %s via %s (%s) failed: %v", e.Host, e.Resolver, e.Endpoint, e.Err)
	}

	return fmt.Sprintf("aoni dns: resolve %s via %s failed: %v", e.Host, e.Resolver, e.Err)
}

// Unwrap returns the underlying wrapped error.
func (e *DNSError) Unwrap() error {
	return e.Err
}

// Timeout reports whether the error was caused by a network timeout.
// This allows DNSError to satisfy the net.Error interface if needed.
func (e *DNSError) Timeout() bool {
	return e.IsTimeout
}

// wrapDNSError is an internal helper to wrap raw errors into a standardized DNSError.
func wrapDNSError(host, resolver, endpoint string, err error) error {
	if err == nil {
		return nil
	}

	// Check if the underlying error is a timeout
	var netErr net.Error

	isTimeout := false
	if errors.As(err, &netErr) {
		isTimeout = netErr.Timeout()
	}

	return &DNSError{
		Host:      host,
		Resolver:  resolver,
		Endpoint:  endpoint,
		Err:       err,
		IsTimeout: isTimeout,
	}
}
