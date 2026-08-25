// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package httpsig

import (
	"errors"
	"time"
)

// Algorithm specifies an RFC 9421 HTTP Signature Algorithm from the IANA registry (§6.2).
type Algorithm string

const (
	// AlgRSAPSSSHA512 represents RSASSA-PSS using SHA-512 (RFC 9421 §3.3.1).
	AlgRSAPSSSHA512 Algorithm = "rsa-pss-sha512"

	// AlgRSAPKCS1v15SHA256 represents RSASSA-PKCS1-v1_5 using SHA-256 (RFC 9421 §3.3.2).
	AlgRSAPKCS1v15SHA256 Algorithm = "rsa-v1_5-sha256"

	// AlgHMACSHA256 represents HMAC using SHA-256 (RFC 9421 §3.3.3).
	AlgHMACSHA256 Algorithm = "hmac-sha256"

	// AlgECDSAP256SHA256 represents ECDSA on NIST P-256 curve with SHA-256 (RFC 9421 §3.3.4).
	AlgECDSAP256SHA256 Algorithm = "ecdsa-p256-sha256"

	// AlgECDSAP384SHA384 represents ECDSA on NIST P-384 curve with SHA-384 (RFC 9421 §3.3.5).
	AlgECDSAP384SHA384 Algorithm = "ecdsa-p384-sha384"

	// AlgEd25519 represents EdDSA on curve edwards25519 (RFC 9421 §3.3.6).
	AlgEd25519 Algorithm = "ed25519"
)

// Standard HTTP Message Signatures (RFC 9421) and Digest Fields (RFC 9530) header names.
const (
	// HeaderSignatureInput carries the covered components and signature parameters (RFC 9421 §4.1).
	HeaderSignatureInput = "Signature-Input"

	// HeaderSignature carries the binary signature byte sequence (RFC 9421 §4.2).
	HeaderSignature = "Signature"

	// HeaderAcceptSignature is used to request a signature on subsequent messages (RFC 9421 §5.1).
	HeaderAcceptSignature = "Accept-Signature"

	// HeaderContentDigest contains a cryptographic digest of the message content (RFC 9530 §2).
	HeaderContentDigest = "Content-Digest"

	// HeaderReprDigest contains a cryptographic digest of the representation (RFC 9530 §3).
	HeaderReprDigest = "Repr-Digest"
)

// Standard Derived Component Names (RFC 9421 §2.2 & §6.4).
const (
	// CompMethod derives the HTTP request method (RFC 9421 §2.2.1).
	CompMethod = "@method"

	// CompTargetURI derives the full target URI of the request (RFC 9421 §2.2.2).
	CompTargetURI = "@target-uri"

	// CompAuthority derives the normalized authority (host:port) of the target URI (RFC 9421 §2.2.3).
	CompAuthority = "@authority"

	// CompScheme derives the lowercase scheme of the target URI (RFC 9421 §2.2.4).
	CompScheme = "@scheme"

	// CompRequestTarget derives the full request target line component (RFC 9421 §2.2.5).
	CompRequestTarget = "@request-target"

	// CompPath derives the absolute path of the target URI (RFC 9421 §2.2.6).
	CompPath = "@path"

	// CompQuery derives the normalized query string including leading '?' (RFC 9421 §2.2.7).
	CompQuery = "@query"

	// CompQueryParam derives a parsed query parameter via the "name" parameter (RFC 9421 §2.2.8).
	CompQueryParam = "@query-param"

	// CompStatus derives the 3-digit numeric HTTP response status code (RFC 9421 §2.2.9).
	CompStatus = "@status"

	// CompSignatureParams is the mandatory final line in the signature base (RFC 9421 §2.3).
	CompSignatureParams = "@signature-params"
)

// Standard Sentinel Errors.
var (
	// ErrInvalidSignature indicates cryptographic verification of the signature failed.
	ErrInvalidSignature = errors.New("httpsig: signature verification failed")

	// ErrSignatureExpired indicates the signature expiration timestamp has passed.
	ErrSignatureExpired = errors.New("httpsig: signature has expired")

	// ErrSignatureTooOld indicates the signature creation timestamp is older than allowed max age.
	ErrSignatureTooOld = errors.New("httpsig: signature creation time is older than maximum permitted age")

	// ErrSignatureInFuture indicates the signature creation timestamp is in the future beyond allowed skew.
	ErrSignatureInFuture = errors.New("httpsig: signature creation time is in the future")

	// ErrMissingComponent indicates a covered component or HTTP header was not present in the message.
	ErrMissingComponent = errors.New("httpsig: required component missing from message")

	// ErrUnsupportedAlgorithm indicates an unrecognized or disallowed signing algorithm.
	ErrUnsupportedAlgorithm = errors.New("httpsig: unsupported or disallowed signature algorithm")

	// ErrMissingSignature indicates the Signature or Signature-Input header is missing from the message.
	ErrMissingSignature = errors.New("httpsig: Signature or Signature-Input header missing")

	// ErrLabelMismatch indicates the requested signature label was not found in Signature or Signature-Input.
	ErrLabelMismatch = errors.New("httpsig: signature label mismatch between Signature and Signature-Input")

	// ErrKeyMismatch indicates the provided key material does not match the algorithm or key ID.
	ErrKeyMismatch = errors.New("httpsig: key material mismatch")

	// ErrDuplicateComponent indicates a component identifier was listed more than once in covered components.
	ErrDuplicateComponent = errors.New("httpsig: duplicate component identifier in covered components list")

	// ErrInvalidStructuredField indicates a structured field value was malformed according to RFC 8941.
	ErrInvalidStructuredField = errors.New("httpsig: malformed structured field syntax")
)

// SignatureParams represents the parsed or configured parameters for an HTTP Message Signature (RFC 9421 §2.3).
type SignatureParams struct {
	// Label is the dictionary key identifying this signature (default: "sig1").
	Label string

	// Components is the ordered set of component identifiers covered by the signature.
	// Example: []string{"@method", "@authority", "@path", "content-digest"}
	Components []string

	// Created is the Unix timestamp in seconds when the signature was generated (0 if omitted).
	Created int64

	// Expires is the Unix timestamp in seconds after which the signature is invalid (0 if omitted).
	Expires int64

	// Nonce is a unique random string protecting against replay attacks.
	Nonce string

	// Alg is the declared HTTP Signature Algorithm (e.g. "rsa-pss-sha512", "ed25519").
	Alg Algorithm

	// KeyID is an identifier for the key material used to sign the message.
	KeyID string

	// Tag is an application-specific tag identifying the signature context or API domain.
	Tag string
}

// Signer signs an HTTP message signature base according to an RFC 9421 cryptographic algorithm.
type Signer interface {
	// Algorithm returns the registered algorithm identifier.
	Algorithm() Algorithm

	// KeyID returns the key identifier (may be empty).
	KeyID() string

	// Sign computes the cryptographic signature over the canonical signature base bytes.
	Sign(base []byte) ([]byte, error)
}

// Verifier verifies an HTTP message signature base according to an RFC 9421 cryptographic algorithm.
type Verifier interface {
	// Algorithm returns the registered algorithm identifier.
	Algorithm() Algorithm

	// KeyID returns the key identifier (may be empty).
	KeyID() string

	// Verify checks whether signature is valid for the canonical signature base bytes.
	Verify(base, sig []byte) error
}

// SignConfig specifies configuration options for generating an HTTP Message Signature.
type SignConfig struct {
	// Label is the signature dictionary key (default: "sig1").
	Label string

	// Signer is the cryptographic signer implementation.
	Signer Signer

	// Components is the list of component identifiers to sign.
	// If empty, defaults to: []string{"@method", "@authority", "@path"}.
	Components []string

	// Created specifies the signature creation time. If zero, time.Now() is used.
	Created time.Time

	// Expires specifies the optional expiration time.
	Expires time.Time

	// Nonce specifies an optional random nonce.
	Nonce string

	// Tag specifies an optional application-specific tag.
	Tag string
}

// VerifyConfig specifies configuration options and policies for verifying HTTP Message Signatures.
type VerifyConfig struct {
	// Label specifies the expected signature label to verify. If empty, the first signature is verified.
	Label string

	// Verifier provides the cryptographic verification routine.
	Verifier Verifier

	// RequiredComponents specifies component identifiers that MUST be covered by the signature.
	RequiredComponents []string

	// MaxAge specifies the maximum allowed age from the signature's created timestamp (0 disables check).
	MaxAge time.Duration

	// AllowedClockSkew specifies tolerated clock skew for created/expires checks (default: 1 minute).
	AllowedClockSkew time.Duration

	// RequiredTag specifies a mandatory application tag that must match the signature params.
	RequiredTag string
}
