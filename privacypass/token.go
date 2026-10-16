// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package privacypass implements the Privacy Pass architecture (RFC 9576),
// HTTP Authentication Scheme (RFC 9577), and Issuance Protocols (RFC 9578)
// for Privacy Pass and Private Access Tokens (PAT).
//
// The package provides silicon-grade, zero-allocation binary wire encoders/decoders,
// RFC 9110-compliant challenge and authorization header parsers, and domain models
// for both publicly verifiable Blind RSA and privately verifiable VOPRF token types.
package privacypass

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// Standard IANA Privacy Pass Token Types (RFC 9577 §7, RFC 9578 §7).
const (
	// TokenTypeVOPRF identifies Privately Verifiable VOPRF (P-384, SHA-384) tokens (RFC 9578 §5).
	//
	// Invariants:
	//   - Codepoint: 0x0001 (IANA Privacy Pass Token Types Registry)
	//   - Verifiable only by the token issuer possessing the private key.
	TokenTypeVOPRF uint16 = 0x0001

	// TokenTypeBlindRSA identifies Publicly Verifiable Blind RSA (2048-bit) tokens (RFC 9578 §6).
	//
	// Invariants:
	//   - Codepoint: 0x0002 (IANA Privacy Pass Token Types Registry)
	//   - Uses RSABSSA-SHA384-PSS-Deterministic signature verification.
	//   - Publicly verifiable by any origin holding the issuer SubjectPublicKeyInfo (SPKI).
	TokenTypeBlindRSA uint16 = 0x0002
)

// Fixed wire format dimensions (RFC 9577 §2.1-2.2, RFC 9578 §3, §5.1).
const (
	// NonceSize is the required length in octets of a token nonce (RFC 9577 §2.2).
	NonceSize = 32

	// ChallengeDigestSize is the required length in octets of the TokenChallenge SHA-256 digest (RFC 9577 §2.2).
	ChallengeDigestSize = 32

	// TokenKeyIDSize is the required length in octets of the Issuer public key SHA-256 identifier (RFC 9577 §2.2).
	TokenKeyIDSize = 32

	// TokenInputSize is the exact length in octets of a serialized TokenInput structure (RFC 9578 §3).
	// Calculated as: token_type(2) + nonce(32) + challenge_digest(32) + token_key_id(32) = 98 octets.
	TokenInputSize = 2 + NonceSize + ChallengeDigestSize + TokenKeyIDSize

	// MinTokenChallengeSize is the minimum valid length in octets of a serialized TokenChallenge (RFC 9577 §2.1).
	// Calculated as: token_type(2) + issuer_len(2) + issuer(min 1) + rc_len(1) + orig_len(2) = 8 octets.
	MinTokenChallengeSize = 8

	// MinTokenRequestHeaderSize is the fixed size in octets of a TokenRequest header before blinded_msg (RFC 9578 §5.1).
	// Calculated as: token_type(2) + truncated_token_key_id(1) = 3 octets.
	MinTokenRequestHeaderSize = 3

	// MinTokenSize is the minimum valid length in octets of a serialized Token structure (RFC 9577 §2.2).
	// A token must contain at least the 98-byte TokenInput preimage and an authenticator.
	MinTokenSize = TokenInputSize
)

// MIME Media Types for Privacy Pass Issuance Protocol (RFC 9578 §4).
const (
	// MediaTypeTokenRequest is the HTTP Content-Type for Privacy Pass token issuance requests (RFC 9578 §4).
	MediaTypeTokenRequest = "application/private-token-request"

	// MediaTypeTokenResponse is the HTTP Content-Type for Privacy Pass token issuance responses (RFC 9578 §4).
	MediaTypeTokenResponse = "application/private-token-response"
)

// Protocol validation errors.
var (
	// ErrTruncatedWire is returned when a binary wire payload contains fewer octets than required.
	ErrTruncatedWire = errors.New("privacypass: truncated wire buffer")

	// ErrTruncatedChallenge is returned when a TokenChallenge wire payload is truncated.
	ErrTruncatedChallenge = fmt.Errorf("%w: TokenChallenge", ErrTruncatedWire)

	// ErrTruncatedTokenRequest is returned when a TokenRequest wire payload is truncated.
	ErrTruncatedTokenRequest = fmt.Errorf("%w: TokenRequest", ErrTruncatedWire)

	// ErrTruncatedTokenResponse is returned when a TokenResponse wire payload is empty or truncated.
	ErrTruncatedTokenResponse = fmt.Errorf("%w: TokenResponse", ErrTruncatedWire)

	// ErrTruncatedToken is returned when a Token wire payload contains fewer than 98 octets.
	ErrTruncatedToken = fmt.Errorf("%w: Token", ErrTruncatedWire)

	// ErrMalformedChallenge is returned when challenge syntax or binary structure violates RFC 9577.
	ErrMalformedChallenge = errors.New("privacypass: malformed challenge structure")

	// ErrMalformedToken is returned when token syntax or binary structure violates RFC 9577 §2.2.
	ErrMalformedToken = errors.New("privacypass: malformed token structure")

	// ErrMalformedAuthorization is returned when an Authorization header does not conform to RFC 9577 §2.2.
	ErrMalformedAuthorization = errors.New("privacypass: malformed authorization header")

	// ErrEmptyIssuerName is returned when TokenChallenge issuer_name has length zero.
	ErrEmptyIssuerName = fmt.Errorf("%w: issuer_name cannot be empty", ErrMalformedChallenge)

	// ErrInvalidRedemptionContextLength is returned when redemption_context length is neither 0 nor 32.
	ErrInvalidRedemptionContextLength = fmt.Errorf(
		"%w: redemption_context length must be 0 or 32",
		ErrMalformedChallenge,
	)

	// ErrInvalidRedemptionContext is an alias for ErrInvalidRedemptionContextLength.
	ErrInvalidRedemptionContext = ErrInvalidRedemptionContextLength

	// ErrInvalidContextLength is an alias for ErrInvalidRedemptionContextLength.
	ErrInvalidContextLength = ErrInvalidRedemptionContextLength

	// ErrEmptyBlindedMsg is returned when a TokenRequest blinded_msg slice is empty.
	ErrEmptyBlindedMsg = errors.New("privacypass: blinded_msg cannot be empty")

	// ErrEmptyAuthenticator is returned when a Token authenticator slice is empty.
	ErrEmptyAuthenticator = fmt.Errorf("%w: authenticator cannot be empty", ErrMalformedToken)

	// ErrInvalidTokenInputLength is returned when a TokenInput buffer is not exactly 98 octets.
	ErrInvalidTokenInputLength = errors.New("privacypass: TokenInput payload must be exactly 98 bytes")

	// ErrInvalidTokenInput is an alias for ErrInvalidTokenInputLength.
	ErrInvalidTokenInput = ErrInvalidTokenInputLength

	// ErrUnsupportedTokenType is returned when encountering an unrecognized or unhandled token type codepoint.
	ErrUnsupportedTokenType = errors.New("privacypass: unsupported token type")

	// ErrNoChallenge is returned when a WWW-Authenticate header is empty or contains only whitespace.
	ErrNoChallenge = errors.New("privacypass: no challenge header present")

	// ErrNoChallengeFound is an alias for ErrNoChallenge.
	ErrNoChallengeFound = ErrNoChallenge

	// ErrNoPrivateTokenChallenge is returned when no PrivateToken authentication scheme is found in the challenge header.
	ErrNoPrivateTokenChallenge = errors.New("privacypass: no PrivateToken challenge found")

	// ErrMissingChallengeParam is returned when the mandatory 'challenge' attribute is missing from PrivateToken challenge.
	ErrMissingChallengeParam = fmt.Errorf(
		"%w: missing required 'challenge' parameter",
		ErrMalformedChallenge,
	)

	// ErrMissingTokenParam is returned when the mandatory 'token' attribute is missing from Authorization header.
	ErrMissingTokenParam = fmt.Errorf(
		"%w: missing required 'token' parameter",
		ErrMalformedAuthorization,
	)

	// ErrInvalidEncoding is returned when base64url decoding fails on challenge, token, or token-key values.
	ErrInvalidEncoding = errors.New("privacypass: invalid base64url encoding")

	// ErrInvalidBase64 is an alias for ErrInvalidEncoding.
	ErrInvalidBase64 = ErrInvalidEncoding

	// ErrExtraneousData is returned when unexpected trailing octets remain after unmarshaling wire structures.
	ErrExtraneousData = fmt.Errorf("%w: extraneous trailing bytes in wire payload", ErrMalformedChallenge)

	// ErrNilToken is returned when attempting to format or marshal a nil Token pointer.
	ErrNilToken = errors.New("privacypass: token cannot be nil")
)

// TokenChallenge represents an RFC 9577 §2.1 authentication challenge issued by an Origin in WWW-Authenticate.
//
// Wire Representation (RFC 8446 Presentation Language):
//
//	struct {
//	    uint16_t token_type;
//	    opaque issuer_name<1..2^16-1>;
//	    opaque redemption_context<0..32>;
//	    opaque origin_info<0..2^16-1>;
//	} TokenChallenge;
//
// Invariants:
//   - IssuerName length MUST be in [1, 65535].
//   - RedemptionContext length MUST be either exactly 0 (empty) or exactly 32 octets.
//   - OriginInfo length MUST be in [0, 65535].
//   - Digest is the SHA-256 hash of the complete raw wire encoding.
type TokenChallenge struct {
	// TokenType identifies the cryptographic issuance protocol (e.g. TokenTypeBlindRSA or TokenTypeVOPRF).
	TokenType uint16

	// IssuerName is the canonical DNS hostname or authority of the Token Issuer (e.g. "issuer.example.com").
	IssuerName string

	// RedemptionContext contains optional 32-byte context-binding data. Must be nil or exactly 32 octets.
	RedemptionContext []byte

	// OriginInfo specifies the target origin (e.g. "https://api.example.com"). Empty if target is the challenger.
	OriginInfo string

	// Digest is the cached SHA-256 digest of the raw binary wire encoding of this challenge.
	Digest [ChallengeDigestSize]byte

	// Raw contains the complete binary wire representation of the challenge.
	Raw []byte

	// TokenKey contains optional DER-encoded SubjectPublicKeyInfo (SPKI) extracted from WWW-Authenticate auth-params.
	TokenKey []byte

	// TokenKeyID contains the SHA-256 hash of TokenKey if TokenKey was present.
	TokenKeyID [TokenKeyIDSize]byte

	// MaxAge specifies the challenge validity lifetime hint extracted from the 'max-age' auth-param.
	MaxAge time.Duration
}

// TokenInput represents the internal 98-byte preimage input to cryptographic blinding
// and verification operations defined in RFC 9578 §3.
//
// Wire Representation:
//
//	struct {
//	    uint16_t token_type;
//	    opaque nonce[32];
//	    opaque challenge_digest[32];
//	    opaque token_key_id[32];
//	} TokenInput;
//
// Invariants:
//   - Exactly 98 octets.
//   - Can be created and serialized with 0 heap allocations via [BuildTokenInput].
type TokenInput struct {
	// TokenType echoes the challenge token type (2 octets, big-endian).
	TokenType uint16

	// Nonce is the client-generated random nonce (32 octets).
	Nonce [NonceSize]byte

	// ChallengeDigest is the SHA-256 digest of the raw TokenChallenge (32 octets).
	ChallengeDigest [ChallengeDigestSize]byte

	// TokenKeyID is the SHA-256 digest of the Issuer's SubjectPublicKeyInfo (32 octets).
	TokenKeyID [TokenKeyIDSize]byte
}

// TokenRequest represents the binary request payload sent to the Token Issuer per RFC 9578 §5.1 / §6.
//
// Wire Representation:
//
//	struct {
//	    uint16_t token_type;
//	    uint8_t  truncated_token_key_id;
//	    opaque   blinded_msg[Nk];
//	} TokenRequest;
//
// Invariants:
//   - TruncatedTokenKeyID is the least significant byte of TokenKeyID (TokenKeyID[31]).
//   - BlindedMsg must be non-empty (e.g., 256 octets for Blind RSA-2048, 49 octets for VOPRF P-384).
type TokenRequest struct {
	// TokenType identifies the cryptographic issuance protocol.
	TokenType uint16

	// TruncatedTokenKeyID is the least significant byte of the Issuer's TokenKeyID (TokenKeyID[31]).
	TruncatedTokenKeyID uint8

	// BlindedMsg is the blinded message payload sent to the Token Issuer.
	BlindedMsg []byte
}

// TokenResponse represents the binary response payload returned by the Token Issuer per RFC 9578 §5.2 / §6.
//
// Wire Representation:
//
//	struct {
//	    uint8_t blind_sig[Nk];
//	} TokenResponse;
//
// Invariants:
//   - Contains the raw blind cryptographic signature or evaluation returned by the Issuer.
//   - For Blind RSA-2048, BlindSig is exactly 256 octets.
type TokenResponse struct {
	// BlindSig is the blind signature or evaluation returned by the Token Issuer.
	BlindSig []byte
}

// Token represents the fully assembled, unblinded authentication token redeemed at the Origin per RFC 9577 §2.2.
//
// Wire Representation:
//
//	struct {
//	    uint16_t token_type;
//	    opaque   nonce[32];
//	    opaque   challenge_digest[32];
//	    opaque   token_key_id[32];
//	    opaque   authenticator[Na];
//	} Token;
//
// Structural Identity:
//   - The first 98 octets (Token[0:98]) are byte-for-byte identical to [TokenInput].
//   - An Origin can verify Authenticator directly against Token[0:98] without intermediate allocations.
type Token struct {
	// TokenType echoes the challenge token type (2 octets, big-endian).
	TokenType uint16

	// Nonce is the client random nonce stored in the Origin replay cache (32 octets).
	Nonce [NonceSize]byte

	// ChallengeDigest is the SHA-256 digest of the TokenChallenge (32 octets).
	ChallengeDigest [ChallengeDigestSize]byte

	// TokenKeyID is the SHA-256 digest of the Issuer's SubjectPublicKeyInfo (32 octets).
	TokenKeyID [TokenKeyIDSize]byte

	// Authenticator is the unblinded signature or MAC tag proving issuance (e.g. 256 octets for Blind RSA-2048).
	Authenticator []byte
}

// BuildTokenInput constructs a stack-allocated, zero-allocation 98-byte TokenInput array
// according to RFC 9578 §3.
//
// Invariants:
//   - Zero heap allocations (returned by value as a fixed [98]byte array).
//   - Big-endian byte order for tokenType.
//
// Example:
//
//	ti := BuildTokenInput(TokenTypeBlindRSA, nonce, challengeDigest, tokenKeyID)
//	digest := sha512.Sum384(ti[:])
func BuildTokenInput(
	tokenType uint16,
	nonce [NonceSize]byte,
	challengeDigest [ChallengeDigestSize]byte,
	tokenKeyID [TokenKeyIDSize]byte,
) [TokenInputSize]byte {
	var out [TokenInputSize]byte
	binary.BigEndian.PutUint16(out[0:2], tokenType)
	copy(out[2:34], nonce[:])
	copy(out[34:66], challengeDigest[:])
	copy(out[66:98], tokenKeyID[:])

	return out
}

// TokenInput returns the 98-byte TokenInput preimage representation of this Token
// without heap allocation.
//
// Invariants:
//   - Zero heap allocations.
func (t *Token) TokenInput() [TokenInputSize]byte {
	return BuildTokenInput(t.TokenType, t.Nonce, t.ChallengeDigest, t.TokenKeyID)
}

// TruncateKeyID extracts the least significant byte of a 32-byte tokenKeyID for TokenRequest framing.
//
// Invariants:
//   - Returns tokenKeyID[31].
func TruncateKeyID(tokenKeyID [TokenKeyIDSize]byte) uint8 {
	return tokenKeyID[TokenKeyIDSize-1]
}

// MarshalBinary implements the [encoding.BinaryMarshaler] interface for [Token].
func (t *Token) MarshalBinary() ([]byte, error) {
	return MarshalToken(t)
}

// UnmarshalBinary implements the [encoding.BinaryUnmarshaler] interface for [Token].
func (t *Token) UnmarshalBinary(data []byte) error {
	tok, err := UnmarshalToken(data)
	if err != nil {
		return err
	}

	*t = *tok

	return nil
}

// MarshalBinary implements the [encoding.BinaryMarshaler] interface for [TokenChallenge].
func (c *TokenChallenge) MarshalBinary() ([]byte, error) {
	return MarshalTokenChallenge(c)
}

// UnmarshalBinary implements the [encoding.BinaryUnmarshaler] interface for [TokenChallenge].
func (c *TokenChallenge) UnmarshalBinary(data []byte) error {
	tc, err := UnmarshalTokenChallenge(data)
	if err != nil {
		return err
	}

	*c = *tc

	return nil
}
