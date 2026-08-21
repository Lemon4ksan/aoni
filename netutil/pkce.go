// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil

import "github.com/lemon4ksan/aoni/netutil/pkce"

// RFC 7636 §4.2 & RFC 9700 §2.1: PKCE Code Challenge Methods.
const (
	// CodeChallengeMethodS256 represents the SHA-256 code challenge transformation (RFC 7636 §4.2).
	CodeChallengeMethodS256 = pkce.MethodS256

	// CodeChallengeMethodPlain represents the identity transformation (RFC 7636 §4.2).
	CodeChallengeMethodPlain = pkce.MethodPlain
)

// GeneratePKCEVerifier creates a cryptographically secure, high-entropy PKCE code verifier (RFC 7636 §4.1).
// For the dedicated subpackage, see [github.com/lemon4ksan/aoni/netutil/pkce].
func GeneratePKCEVerifier(length int) (string, error) {
	return pkce.GenerateVerifier(length)
}

// ComputePKCEChallenge calculates the PKCE code_challenge from a code_verifier (RFC 7636 §4.2, RFC 9700 §2.1).
func ComputePKCEChallenge(verifier string, method ...string) (string, error) {
	return pkce.ComputeChallenge(verifier, method...)
}

// ValidatePKCE performs constant-time validation of a code_verifier against a code_challenge (RFC 7636 §4.6).
func ValidatePKCE(verifier, challenge string, method ...string) bool {
	return pkce.Validate(verifier, challenge, method...)
}

// MatchRedirectURI verifies whether requestURI matches registeredURI according to RFC 9700 §2.1 & §4.1.3.
func MatchRedirectURI(registeredURI, requestURI string) bool {
	return pkce.MatchRedirectURI(registeredURI, requestURI)
}
