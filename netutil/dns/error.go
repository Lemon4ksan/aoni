// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"errors"
	"fmt"
	"net"
)

// ResolutionError represents an error occurring during DNS resolution in the aoni package.
type ResolutionError struct {
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
func (e *ResolutionError) Error() string {
	if e.Endpoint != "" {
		return fmt.Sprintf("aoni dns: resolve %s via %s (%s) failed: %v", e.Host, e.Resolver, e.Endpoint, e.Err)
	}

	return fmt.Sprintf("aoni dns: resolve %s via %s failed: %v", e.Host, e.Resolver, e.Err)
}

// Unwrap returns the underlying wrapped error.
func (e *ResolutionError) Unwrap() error {
	return e.Err
}

// Timeout reports whether the error was caused by a network timeout.
// This allows DNSError to satisfy the [net.Error] interface if needed.
func (e *ResolutionError) Timeout() bool {
	return e.IsTimeout
}

// Temporary reports whether the error is temporary.
// This is required to satisfy the [net.Error] interface.
func (e *ResolutionError) Temporary() bool {
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

	return &ResolutionError{
		Host:      host,
		Resolver:  resolver,
		Endpoint:  endpoint,
		Err:       err,
		IsTimeout: isTimeout,
	}
}
