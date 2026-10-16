// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package privacypass

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
)

// MarshalTokenChallenge serializes a [TokenChallenge] into its RFC 9577 §2.1 wire presentation format.
//
// Wire layout:
//
//	token_type (2B) || issuer_name_length (2B) || issuer_name ||
//	redemption_context_length (1B) || redemption_context (0 or 32B) ||
//	origin_info_length (2B) || origin_info
//
// Invariants:
//   - c cannot be nil.
//   - issuer_name length must be in [1, 65535].
//   - redemption_context length must be strictly 0 or 32 octets.
//   - origin_info length must be in [0, 65535].
//   - Big-endian byte order for multi-byte integers.
func MarshalTokenChallenge(c *TokenChallenge) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("privacypass: nil challenge: %w", ErrTruncatedChallenge)
	}

	issuerLen := len(c.IssuerName)
	if issuerLen == 0 || issuerLen > 65535 {
		return nil, fmt.Errorf("%w: length %d out of bounds [1, 65535]", ErrEmptyIssuerName, issuerLen)
	}

	rcLen := len(c.RedemptionContext)
	if rcLen != 0 && rcLen != 32 {
		return nil, fmt.Errorf("%w: got %d", ErrInvalidRedemptionContextLength, rcLen)
	}

	originLen := len(c.OriginInfo)
	if originLen > 65535 {
		return nil, fmt.Errorf("%w: origin_info length %d exceeds 65535", ErrMalformedChallenge, originLen)
	}

	totalLen := 2 + 2 + issuerLen + 1 + rcLen + 2 + originLen
	buf := make([]byte, totalLen)

	binary.BigEndian.PutUint16(buf[0:2], c.TokenType)
	binary.BigEndian.PutUint16(buf[2:4], uint16(issuerLen))
	copy(buf[4:4+issuerLen], c.IssuerName)

	offset := 4 + issuerLen
	buf[offset] = uint8(rcLen)
	offset++

	if rcLen > 0 {
		copy(buf[offset:offset+rcLen], c.RedemptionContext)
		offset += rcLen
	}

	binary.BigEndian.PutUint16(buf[offset:offset+2], uint16(originLen))
	offset += 2

	if originLen > 0 {
		copy(buf[offset:offset+originLen], c.OriginInfo)
	}

	return buf, nil
}

// UnmarshalTokenChallenge parses a raw wire payload into a [TokenChallenge] per RFC 9577 §2.1.
//
// Computes and caches the SHA-256 challenge digest on the struct, and preserves
// the raw wire bytes in the Raw field.
//
// Invariants:
//   - Rejects payloads shorter than MinTokenChallengeSize (8 octets).
//   - Rejects empty issuer_name.
//   - Rejects redemption_context lengths other than 0 or 32 octets.
//   - Rejects unexpected trailing octets.
//   - Panic-safe on all pathological inputs.
func UnmarshalTokenChallenge(raw []byte) (*TokenChallenge, error) {
	if len(raw) < MinTokenChallengeSize {
		return nil, fmt.Errorf("%w: length %d < minimum %d", ErrTruncatedChallenge, len(raw), MinTokenChallengeSize)
	}

	tokenType := binary.BigEndian.Uint16(raw[0:2])

	issuerLen := int(binary.BigEndian.Uint16(raw[2:4]))
	if issuerLen == 0 {
		return nil, fmt.Errorf("%w: issuer_name_length is 0", ErrEmptyIssuerName)
	}

	offset := 4
	if len(raw) < offset+issuerLen+1+2 {
		return nil, fmt.Errorf("%w: insufficient bytes for issuer_name and following fields", ErrTruncatedChallenge)
	}

	issuerName := string(raw[offset : offset+issuerLen])
	offset += issuerLen

	rcLen := int(raw[offset])
	offset++

	if rcLen != 0 && rcLen != 32 {
		return nil, fmt.Errorf("%w: got %d (must be 0 or 32)", ErrInvalidRedemptionContextLength, rcLen)
	}

	if len(raw) < offset+rcLen+2 {
		return nil, fmt.Errorf(
			"%w: insufficient bytes for redemption_context and origin_info_length",
			ErrTruncatedChallenge,
		)
	}

	var redemptionContext []byte
	if rcLen > 0 {
		redemptionContext = slices.Clone(raw[offset : offset+rcLen])
		offset += rcLen
	}

	originLen := int(binary.BigEndian.Uint16(raw[offset : offset+2]))
	offset += 2

	if len(raw) < offset+originLen {
		return nil, fmt.Errorf("%w: insufficient bytes for origin_info", ErrTruncatedChallenge)
	}

	var originInfo string
	if originLen > 0 {
		originInfo = string(raw[offset : offset+originLen])
		offset += originLen
	}

	// Exact wire format matching: reject unexpected trailing octets
	if offset != len(raw) {
		return nil, fmt.Errorf("%w: unexpected %d trailing octets", ErrExtraneousData, len(raw)-offset)
	}

	return &TokenChallenge{
		TokenType:         tokenType,
		IssuerName:        issuerName,
		RedemptionContext: redemptionContext,
		OriginInfo:        originInfo,
		Digest:            sha256.Sum256(raw),
		Raw:               slices.Clone(raw),
	}, nil
}

// MarshalTokenInput serializes a [TokenInput] into its fixed 98-byte representation (RFC 9578 §3).
//
// Invariants:
//   - Returns a stack-allocated [98]byte value with zero heap allocations.
func MarshalTokenInput(ti *TokenInput) [TokenInputSize]byte {
	if ti == nil {
		return [TokenInputSize]byte{}
	}

	return BuildTokenInput(ti.TokenType, ti.Nonce, ti.ChallengeDigest, ti.TokenKeyID)
}

// UnmarshalTokenInput parses a raw 98-byte slice into a [TokenInput] (RFC 9578 §3).
//
// Invariants:
//   - raw must be exactly 98 octets.
func UnmarshalTokenInput(raw []byte) (*TokenInput, error) {
	if len(raw) != TokenInputSize {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidTokenInputLength, TokenInputSize, len(raw))
	}

	var ti TokenInput

	ti.TokenType = binary.BigEndian.Uint16(raw[0:2])
	copy(ti.Nonce[:], raw[2:34])
	copy(ti.ChallengeDigest[:], raw[34:66])
	copy(ti.TokenKeyID[:], raw[66:98])

	return &ti, nil
}

// MarshalTokenRequest serializes a [TokenRequest] into its RFC 9578 §5.1 / §6 wire format.
//
// Wire layout:
//
//	token_type (2B) || truncated_token_key_id (1B) || blinded_msg (Nk bytes)
//
// Invariants:
//   - tr cannot be nil.
//   - blinded_msg must be non-empty.
func MarshalTokenRequest(tr *TokenRequest) ([]byte, error) {
	if tr == nil {
		return nil, fmt.Errorf("privacypass: nil request: %w", ErrTruncatedTokenRequest)
	}

	if len(tr.BlindedMsg) == 0 {
		return nil, fmt.Errorf("%w: blinded_msg is empty", ErrEmptyBlindedMsg)
	}

	totalLen := MinTokenRequestHeaderSize + len(tr.BlindedMsg)
	buf := make([]byte, totalLen)

	binary.BigEndian.PutUint16(buf[0:2], tr.TokenType)
	buf[2] = tr.TruncatedTokenKeyID
	copy(buf[3:], tr.BlindedMsg)

	return buf, nil
}

// UnmarshalTokenRequest parses a raw wire payload into a [TokenRequest] per RFC 9578 §5.1 / §6.
//
// Invariants:
//   - raw must be strictly greater than MinTokenRequestHeaderSize (3 octets).
//   - BlindedMsg consumes the remainder of the payload.
func UnmarshalTokenRequest(raw []byte) (*TokenRequest, error) {
	if len(raw) <= MinTokenRequestHeaderSize {
		return nil, fmt.Errorf(
			"%w: length %d <= header size %d",
			ErrTruncatedTokenRequest,
			len(raw),
			MinTokenRequestHeaderSize,
		)
	}

	return &TokenRequest{
		TokenType:           binary.BigEndian.Uint16(raw[0:2]),
		TruncatedTokenKeyID: raw[2],
		BlindedMsg:          slices.Clone(raw[3:]),
	}, nil
}

// MarshalTokenResponse serializes a [TokenResponse] into its RFC 9578 §5.2 / §6 wire format.
//
// The payload is the raw unadorned blind signature or evaluation bytes.
//
// Invariants:
//   - tr cannot be nil.
//   - BlindSig must be non-empty.
func MarshalTokenResponse(tr *TokenResponse) ([]byte, error) {
	if tr == nil || len(tr.BlindSig) == 0 {
		return nil, fmt.Errorf("%w: blind_sig is empty", ErrTruncatedTokenResponse)
	}

	return slices.Clone(tr.BlindSig), nil
}

// UnmarshalTokenResponse parses a raw wire payload into a [TokenResponse] per RFC 9578 §5.2 / §6.
//
// Invariants:
//   - raw must be non-empty.
func UnmarshalTokenResponse(raw []byte) (*TokenResponse, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: response body is empty", ErrTruncatedTokenResponse)
	}

	return &TokenResponse{
		BlindSig: slices.Clone(raw),
	}, nil
}

// MarshalToken serializes a [Token] into its RFC 9577 §2.2 / RFC 9578 §3 wire format.
//
// Wire layout:
//
//	token_type (2B) || nonce (32B) || challenge_digest (32B) || token_key_id (32B) || authenticator (Na bytes)
//
// Invariants:
//   - t cannot be nil.
//   - Authenticator must be non-empty.
func MarshalToken(t *Token) ([]byte, error) {
	if t == nil {
		return nil, fmt.Errorf("privacypass: nil token: %w", ErrNilToken)
	}

	if len(t.Authenticator) == 0 {
		return nil, fmt.Errorf("%w: cannot marshal token with empty authenticator", ErrEmptyAuthenticator)
	}

	totalLen := TokenInputSize + len(t.Authenticator)
	buf := make([]byte, totalLen)

	binary.BigEndian.PutUint16(buf[0:2], t.TokenType)
	copy(buf[2:34], t.Nonce[:])
	copy(buf[34:66], t.ChallengeDigest[:])
	copy(buf[66:98], t.TokenKeyID[:])
	copy(buf[98:], t.Authenticator)

	return buf, nil
}

// UnmarshalToken parses a raw wire payload into a [Token] per RFC 9577 §2.2.
//
// Invariants:
//   - raw must be at least MinTokenSize (98 octets).
//   - Authenticator must be non-empty (raw > 98 octets).
func UnmarshalToken(raw []byte) (*Token, error) {
	if len(raw) < MinTokenSize {
		return nil, fmt.Errorf("%w: length %d < minimum %d", ErrTruncatedToken, len(raw), MinTokenSize)
	}

	if len(raw) == MinTokenSize {
		return nil, fmt.Errorf("%w: authenticator is empty", ErrEmptyAuthenticator)
	}

	var t Token

	t.TokenType = binary.BigEndian.Uint16(raw[0:2])
	copy(t.Nonce[:], raw[2:34])
	copy(t.ChallengeDigest[:], raw[34:66])
	copy(t.TokenKeyID[:], raw[66:98])
	t.Authenticator = slices.Clone(raw[TokenInputSize:])

	return &t, nil
}
