// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"errors"
	"fmt"
	"net"
)

var (
	// ErrNXDomain indicates the queried domain name does not exist (RCODE 3 / RFC 2308 §2.1).
	ErrNXDomain = errors.New("aoni/dns: domain name does not exist (NXDOMAIN / RFC 2308)")

	// ErrNODATA indicates the queried domain name exists but has no records of the requested type (RFC 2308 §2.2).
	ErrNODATA = errors.New("aoni/dns: no data for requested record type (NODATA / RFC 2308)")
)

// IsNotFound reports whether err represents an authoritative negative DNS response (NXDOMAIN or NODATA per RFC 2308).
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNXDomain) || errors.Is(err, ErrNODATA) {
		return true
	}

	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok {
		return dnsErr.IsNotFound
	}

	if resErr, ok := errors.AsType[*ResolutionError](err); ok {
		return IsNotFound(resErr.Err)
	}

	return false
}

// IsNXDomain reports whether err represents an authoritative NXDOMAIN Name Error (RFC 8020 / RFC 2308 §2.1).
func IsNXDomain(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNXDomain) {
		return true
	}

	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok {
		return dnsErr.IsNotFound
	}

	if resErr, ok := errors.AsType[*ResolutionError](err); ok {
		return IsNXDomain(resErr.Err)
	}

	return false
}

// ResolutionError represents an error occurring during DNS resolution in the dns package.
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
		return fmt.Sprintf("dns: resolve %s via %s (%s) failed: %v", e.Host, e.Resolver, e.Endpoint, e.Err)
	}

	return fmt.Sprintf("dns: resolve %s via %s failed: %v", e.Host, e.Resolver, e.Err)
}

// Unwrap returns the underlying wrapped error.
func (e *ResolutionError) Unwrap() error {
	return e.Err
}

// Timeout reports whether the error was caused by a network timeout.
// This allows ResolutionError to satisfy the [net.Error] interface.
func (e *ResolutionError) Timeout() bool {
	return e.IsTimeout
}

// Temporary reports whether the error is temporary.
// This is required to satisfy the [net.Error] interface.
func (e *ResolutionError) Temporary() bool {
	return e.IsTimeout
}

// WrapDNSError wraps raw errors into a standardized ResolutionError.
func WrapDNSError(host, resolver, endpoint string, err error) error {
	if err == nil {
		return nil
	}

	isTimeout := false
	if netErr, ok := errors.AsType[net.Error](err); ok {
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
