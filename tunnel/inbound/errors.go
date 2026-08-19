// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inbound

import "errors"

var (
	// ErrServerClosed indicates the inbound proxy server is closed.
	ErrServerClosed = errors.New("aoni inbound: server is closed")

	// ErrInvalidSocks5Header indicates a corrupted or non-SOCKS5 protocol header.
	ErrInvalidSocks5Header = errors.New("aoni inbound: invalid socks5 protocol header")

	// ErrUnsupportedCommand indicates an unsupported SOCKS5 or HTTP proxy command.
	ErrUnsupportedCommand = errors.New("aoni inbound: unsupported proxy command")

	// ErrAuthFailed indicates authentication failure on inbound proxy connection.
	ErrAuthFailed = errors.New("aoni inbound: authentication failed")

	// ErrInvalidHTTPHeader indicates a malformed or non-proxy HTTP header.
	ErrInvalidHTTPHeader = errors.New("aoni inbound: invalid http proxy request header")

	// ErrMITMFailed indicates an error during TLS MITM interception or dynamic certificate generation.
	ErrMITMFailed = errors.New("aoni inbound: tls mitm interception failed")
)
