// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dpop

import (
	"errors"
	"time"
)

// Standard DPoP HTTP Headers and Authentication Schemes (RFC 9449 §4.1, §7.1, §8).
const (
	// HeaderDPoP carries the DPoP Proof JWT in requests (RFC 9449 §4.1).
	HeaderDPoP = "DPoP"

	// HeaderDPoPNonce carries server-provided nonces in responses (RFC 9449 §8 & §9).
	HeaderDPoPNonce = "DPoP-Nonce"

	// HeaderAuthorization carries credentials (e.g. "DPoP <access-token>") (RFC 9449 §7.1).
	HeaderAuthorization = "Authorization"

	// HeaderWWWAuthenticate carries authentication challenges (RFC 9449 §7.1).
	HeaderWWWAuthenticate = "WWW-Authenticate"

	// SchemeDPoP is the HTTP authentication scheme for DPoP access tokens (RFC 9449 §7.1).
	SchemeDPoP = "DPoP"

	// TypeDPoPJWT is the mandatory typ header parameter value for DPoP proof JWTs (RFC 9449 §4.2).
	TypeDPoPJWT = "dpop+jwt"
)

// Standard JWS Algorithms supported in DPoP Proofs (RFC 9449 §4.2 & RFC 7518).
const (
	AlgES256 = "ES256" // ECDSA using P-256 and SHA-256
	AlgES384 = "ES384" // ECDSA using P-384 and SHA-384
	AlgEdDSA = "EdDSA" // EdDSA using Curve edwards25519
	AlgRS256 = "RS256" // RSASSA-PKCS1-v1_5 using SHA-256
	AlgPS256 = "PS256" // RSASSA-PSS using SHA-256
)

// Standard DPoP Error Codes in WWW-Authenticate headers (RFC 9449 §12.2).
const (
	ErrCodeInvalidDPoPProof = "invalid_dpop_proof"
	ErrCodeInvalidToken     = "invalid_token"
	ErrCodeUseDPoPNonce     = "use_dpop_nonce"
)

// Standard Sentinel Errors.
var (
	// ErrInvalidDPoPProof indicates the DPoP Proof JWT is malformed.
	ErrInvalidDPoPProof = errors.New("dpop: invalid dpop proof")

	// ErrInvalidHeader indicates the JOSE header in the DPoP proof is invalid or missing required fields.
	ErrInvalidHeader = errors.New("dpop: invalid dpop header")

	// ErrInvalidClaims indicates required JWT claims (jti, htm, htu, iat) are missing or malformed.
	ErrInvalidClaims = errors.New("dpop: invalid dpop claims")

	// ErrInvalidSignature indicates cryptographic verification of the DPoP proof failed.
	ErrInvalidSignature = errors.New("dpop: signature verification failed")

	// ErrProofExpired indicates the DPoP proof was created outside the accepted validity window.
	ErrProofExpired = errors.New("dpop: proof expired or outside accepted time window")

	// ErrProofInFuture indicates the DPoP proof iat timestamp is in the future beyond allowed skew.
	ErrProofInFuture = errors.New("dpop: proof iat timestamp is in the future")

	// ErrMethodMismatch indicates the htm claim does not match the HTTP request method.
	ErrMethodMismatch = errors.New("dpop: htm claim does not match request method")

	// ErrURIMismatch indicates the htu claim does not match the HTTP request target URI.
	ErrURIMismatch = errors.New("dpop: htu claim does not match request target URI")

	// ErrAccessTokenMismatch indicates the ath claim does not match the hash of the presented access token.
	ErrAccessTokenMismatch = errors.New("dpop: ath claim does not match access token hash")

	// ErrNonceMismatch indicates the nonce claim does not match the required server nonce.
	ErrNonceMismatch = errors.New("dpop: nonce claim does not match required server nonce")

	// ErrUnsupportedKey indicates an unsupported private or public key type or curve.
	ErrUnsupportedKey = errors.New("dpop: unsupported key type or algorithm")
)

// JWK represents a public JSON Web Key (RFC 7517) embedded in a DPoP header.
type JWK struct {
	Kty string `json:"kty"`           // Key Type: "EC", "OKP", "RSA"
	Crv string `json:"crv,omitempty"` // Curve: "P-256", "P-384", "Ed25519"
	X   string `json:"x,omitempty"`   // X Coordinate or Ed25519 Public Key (Base64URL)
	Y   string `json:"y,omitempty"`   // Y Coordinate (Base64URL)
	N   string `json:"n,omitempty"`   // RSA Modulus (Base64URL)
	E   string `json:"e,omitempty"`   // RSA Exponent (Base64URL)
	Alg string `json:"alg,omitempty"` // Algorithm (optional)
	Kid string `json:"kid,omitempty"` // Key ID (optional)
}

// ProofHeader represents the JOSE Header of a DPoP Proof JWT (RFC 9449 §4.2).
type ProofHeader struct {
	Typ string `json:"typ"` // Must be "dpop+jwt"
	Alg string `json:"alg"` // Signature algorithm
	JWK *JWK   `json:"jwk"` // Public key representation
}

// ProofClaims represents the payload claims of a DPoP Proof JWT (RFC 9449 §4.2).
type ProofClaims struct {
	JTI   string `json:"jti"`             // Unique Proof Identifier
	HTM   string `json:"htm"`             // HTTP Method (uppercase, e.g. "POST")
	HTU   string `json:"htu"`             // HTTP Target URI without query/fragment
	IAT   int64  `json:"iat"`             // Issued At timestamp in seconds
	Nonce string `json:"nonce,omitempty"` // Server-Provided Nonce
	ATH   string `json:"ath,omitempty"`   // Access Token Hash: base64url(sha256(access_token))
}

// ProofOptions specifies options for generating a DPoP proof.
type ProofOptions struct {
	// Nonce is the server-provided nonce received from a DPoP-Nonce header.
	Nonce string

	// AccessToken is the OAuth 2.0 access token to bind via the "ath" claim.
	AccessToken string

	// CustomJTI allows overriding the automatically generated unique random JTI string.
	CustomJTI string

	// IAT specifies the issuance time. If zero, time.Now() is used.
	IAT time.Time
}

// VerifierConfig specifies validation policies when verifying DPoP proofs.
type VerifierConfig struct {
	// ExpectedMethod is the expected HTTP request method (e.g. "GET", "POST").
	ExpectedMethod string

	// ExpectedURI is the expected request URL (normalized to strip query/fragment).
	ExpectedURI string

	// AccessToken is the presented access token to validate against the "ath" claim (if applicable).
	AccessToken string

	// ExpectedNonce is the server-expected nonce to validate against the "nonce" claim (if required).
	ExpectedNonce string

	// MaxAge specifies the maximum allowed age of the proof (default: 5 minutes if 0).
	MaxAge time.Duration

	// AllowedClockSkew specifies tolerated clock skew for iat validation (default: 1 minute if 0).
	AllowedClockSkew time.Duration
}
