// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil

import (
	"crypto"
	"net/http"

	"github.com/lemon4ksan/aoni/netutil/dpop"
)

// OAuth 2.0 DPoP (RFC 9449) and JWK (RFC 7517) type aliases.
type (
	// DPoPProofClaims represents payload claims of a DPoP Proof JWT (RFC 9449 §4.2).
	DPoPProofClaims = dpop.ProofClaims

	// DPoPJWK represents a public JSON Web Key (RFC 7517).
	DPoPJWK = dpop.JWK

	// DPoPOptions specifies optional parameters for DPoP proof generation.
	DPoPOptions = dpop.ProofOptions

	// DPoPVerifierConfig holds validation policies for DPoP proof verification.
	DPoPVerifierConfig = dpop.VerifierConfig
)

// Standard DPoP Constants (RFC 9449).
const (
	// DPoPHeader carries the DPoP Proof JWT in requests (RFC 9449 §4.1).
	DPoPHeader = dpop.HeaderDPoP

	// DPoPNonceHeader carries server-provided nonces (RFC 9449 §8 & §9).
	DPoPNonceHeader = dpop.HeaderDPoPNonce

	// DPoPScheme is the HTTP authentication scheme for DPoP access tokens (RFC 9449 §7.1).
	DPoPScheme = dpop.SchemeDPoP
)

// CreateDPoPProof generates a signed DPoP Proof JWT according to RFC 9449 §4.2.
func CreateDPoPProof(privKey crypto.PrivateKey, method, targetURI string, opts ...DPoPOptions) (string, error) {
	return dpop.CreateProof(privKey, method, targetURI, opts...)
}

// CreateDPoPProofForRequest creates a signed DPoP Proof JWT directly from an [*http.Request].
func CreateDPoPProofForRequest(req *http.Request, privKey crypto.PrivateKey, opts ...DPoPOptions) (string, error) {
	return dpop.CreateProofForRequest(req, privKey, opts...)
}

// VerifyDPoPProof verifies an RFC 9449 DPoP Proof JWT string against validation policies.
func VerifyDPoPProof(proofJWT string, cfg DPoPVerifierConfig) (*DPoPProofClaims, *DPoPJWK, error) {
	return dpop.VerifyProof(proofJWT, cfg)
}

// VerifyDPoPProofForRequest extracts and validates the DPoP Proof from an incoming [*http.Request].
func VerifyDPoPProofForRequest(req *http.Request, cfg DPoPVerifierConfig) (*DPoPProofClaims, *DPoPJWK, error) {
	return dpop.VerifyProofForRequest(req, cfg)
}

// ComputeAccessTokenHash calculates the RFC 9449 §4.2 Access Token Hash ("ath") claim.
func ComputeAccessTokenHash(accessToken string) string {
	return dpop.ComputeAccessTokenHash(accessToken)
}

// ComputeJWKThumbprint calculates the RFC 7638 JWK Thumbprint in Base64URL encoding.
func ComputeJWKThumbprint(jwk *DPoPJWK) (string, error) {
	return dpop.ComputeThumbprint(jwk)
}

// FormatDPoPAuth formats an access token into an RFC 9449 §7.1 Authorization header value ("DPoP <token>").
func FormatDPoPAuth(token string) string {
	return dpop.SchemeDPoP + " " + token
}
