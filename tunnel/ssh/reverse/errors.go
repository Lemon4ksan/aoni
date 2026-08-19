// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reverse

import "errors"

var (
	// ErrTunnelNotFound is returned when an incoming public request cannot be mapped to an active reverse SSH tunnel.
	ErrTunnelNotFound = errors.New("aoni ssh reverse: no active tunnel registered for requested host")

	// ErrInvalidForwardPayload is returned when an SSH global request payload fails unmarshaling.
	ErrInvalidForwardPayload = errors.New("aoni ssh reverse: invalid tcpip-forward request payload")

	// ErrTunnelClosed is returned when attempting an operation on an inactive or closed reverse tunnel.
	ErrTunnelClosed = errors.New("aoni ssh reverse: tunnel session is closed")

	// ErrHostAlreadyBound is returned when a requested subdomain or port is already bound by another active tunnel.
	ErrHostAlreadyBound = errors.New("aoni ssh reverse: requested subdomain or port is already bound")

	// ErrInvalidTLSHeader is returned when peeking an incoming TLS stream that does not contain a valid Handshake header.
	ErrInvalidTLSHeader = errors.New("aoni ssh reverse: invalid or non-TLS ClientHello record")
)
