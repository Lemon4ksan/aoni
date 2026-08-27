// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pkce implements RFC 7636 Proof Key for Code Exchange by OAuth Public Clients.
// Core implementation is located in [github.com/lemon4ksan/foundation/net/pkce].
package pkce

import "github.com/lemon4ksan/foundation/net/pkce"

// RFC 7636 §4.2 & RFC 9700 §2.1: PKCE Code Challenge Methods.
const (
	MethodS256  = pkce.MethodS256
	MethodPlain = pkce.MethodPlain
)

// Standard error sentinels.
var (
	ErrVerifierLength = pkce.ErrVerifierLength
	ErrInvalidMethod  = pkce.ErrInvalidMethod
)

// ChallengePair represents a generated PKCE code verifier and its corresponding code challenge.
type ChallengePair = pkce.ChallengePair

// New creates a new [ChallengePair] with a cryptographically secure verifier.
func New(length ...int) (*ChallengePair, error) {
	return pkce.New(length...)
}

// GenerateVerifier generates a cryptographically random URL-safe code verifier.
func GenerateVerifier(length int) (string, error) {
	return pkce.GenerateVerifier(length)
}

// ComputeChallenge computes the code challenge for a given verifier string and method.
func ComputeChallenge(verifier, method string) (string, error) {
	return pkce.ComputeChallenge(verifier, method)
}

// Validate verifies that a code verifier matches the expected code challenge according to method.
func Validate(verifier, challenge, method string) bool {
	return pkce.Validate(verifier, challenge, method)
}
