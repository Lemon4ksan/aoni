// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package masque

import "errors"

var (
	// ErrHandshakeFailed indicates the connect-ip proxy request failed or returned a non-200/101 status code.
	ErrHandshakeFailed = errors.New("aoni masque: connect-ip handshake failed")

	// ErrInvalidCapsule indicates a corrupted or truncated Capsule Protocol frame was received.
	ErrInvalidCapsule = errors.New("aoni masque: invalid capsule format")

	// ErrInvalidURITemplate indicates that the provided MASQUE URI template is malformed.
	ErrInvalidURITemplate = errors.New("aoni masque: invalid uri template")

	// ErrUnsupportedHTTPVersion indicates that connect-ip was attempted on an unsupported transport.
	ErrUnsupportedHTTPVersion = errors.New("aoni masque: unsupported http version for connect-ip")

	// ErrEmptyAddressRequest indicates an ADDRESS_REQUEST capsule contained zero requested addresses.
	ErrEmptyAddressRequest = errors.New("aoni masque: address request capsule cannot be empty")
)
