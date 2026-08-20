// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ca

import "errors"

var (
	// ErrInvalidCAKey is returned when a private key provided for the CA is invalid or unparseable.
	ErrInvalidCAKey = errors.New("aoni/ssh/ca: invalid or corrupted CA private key")

	// ErrInvalidPublicKey is returned when a target public key provided for signing is invalid.
	ErrInvalidPublicKey = errors.New("aoni/ssh/ca: invalid target public key")

	// ErrInvalidCertificate is returned when parsing or validating an OpenSSH certificate fails.
	ErrInvalidCertificate = errors.New("aoni/ssh/ca: invalid or corrupted certificate")

	// ErrCertExpired is returned when attempting an operation on an expired certificate.
	ErrCertExpired = errors.New("aoni/ssh/ca: certificate validity window has expired")

	// ErrInvalidOIDCToken is returned when an OIDC ID token payload or signature validation fails.
	ErrInvalidOIDCToken = errors.New("aoni/ssh/ca: invalid or unverified OIDC ID token")

	// ErrMissingOIDCClaim is returned when the required user claim (e.g. email) is missing from the OIDC token.
	ErrMissingOIDCClaim = errors.New("aoni/ssh/ca: required claim missing from OIDC token")
)
