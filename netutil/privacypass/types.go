// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package privacypass implements the Privacy Pass Architecture (RFC 9576) and
// the PrivateToken HTTP Authentication Scheme (RFC 9577) for anti-bot WAF bypass
// and anonymous zero-knowledge authorization.
package privacypass

import (
	"errors"
	"time"

	"github.com/lemon4ksan/foundation/net/http/header"
)

// Standard HTTP Header field names and authentication schemes (RFC 9577 & W3C).
const (
	// SchemePrivateToken is the IANA-registered HTTP authentication scheme name (RFC 9577 §6.1).
	SchemePrivateToken = "PrivateToken"

	// HeaderSecPrivateStateToken is the W3C Private State Tokens redemption/issuance header name.
	HeaderSecPrivateStateToken = "Sec-Private-State-Token"

	// HeaderWWWAuthenticate specifies standard HTTP challenge header (RFC 9110 §11.6.1).
	HeaderWWWAuthenticate = header.WWWAuthenticate

	// HeaderAuthorization specifies standard HTTP authorization header (RFC 9110 §11.6.2).
	HeaderAuthorization = header.Authorization
)

// Common errors returned by Privacy Pass operations.
var (
	ErrInvalidChallengeData     = errors.New("aoni/privacypass: invalid TokenChallenge binary encoding")
	ErrInvalidTokenData         = errors.New("aoni/privacypass: invalid Token binary encoding")
	ErrUnsupportedTokenType     = errors.New("aoni/privacypass: unsupported token type")
	ErrMissingChallengeParam    = errors.New("aoni/privacypass: missing challenge parameter in WWW-Authenticate")
	ErrInvalidRedemptionContext = errors.New("aoni/privacypass: redemption context length must be 0 or 32 bytes")
	ErrNoTokenAvailable         = errors.New("aoni/privacypass: no valid Privacy Pass token available for challenge")
	ErrOriginMismatch           = errors.New("aoni/privacypass: origin not listed in origin_info")
)

// TokenType represents a 2-octet Privacy Pass issuance protocol identifier in network byte order (RFC 9577 §6.2).
type TokenType uint16

const (
	// TypeBlindRSA represents Privacy Pass V1 Blind RSA Token issuance (RFC 9578).
	TypeBlindRSA TokenType = 0x0001

	// TypePubliclyVerifiable represents Publicly Verifiable Tokens (RFC 9578 §6 / RFC 9577 Appendix A).
	TypePubliclyVerifiable TokenType = 0x0002
)

// Reserved GREASE token types defined in RFC 9577 §6.2.1.
var GreaseTokenTypes = [...]TokenType{
	0x0000, 0x02AA, 0x1132, 0x2E96, 0x3CD3, 0x4473, 0x5A63, 0x6D32,
	0x7F3F, 0x8D07, 0x916B, 0xA6A4, 0xBEAB, 0xC3F3, 0xDA42, 0xE944, 0xF057,
}

// IsGrease returns true if t is a reserved GREASE token type (RFC 9577 §6.2.1).
func (t TokenType) IsGrease() bool {
	for _, g := range GreaseTokenTypes {
		if t == g {
			return true
		}
	}

	return false
}

// TokenChallenge represents the RFC 9577 §2.1.1 TokenChallenge structure sent by Origins.
type TokenChallenge struct {
	TokenType         TokenType
	IssuerName        string
	RedemptionContext []byte // Must be 0 or 32 bytes
	OriginInfo        string // Comma-separated origin hostnames
}

// ChallengeParams encapsulates parsed authentication parameters from a `WWW-Authenticate: PrivateToken ...` header.
type ChallengeParams struct {
	Challenge *TokenChallenge
	TokenKey  []byte
	MaxAge    time.Duration
	Realm     string
	RawParam  string
}

// Token represents the RFC 9577 §2.2.1 Token structure presented during redemption.
type Token struct {
	TokenType       TokenType
	Nonce           [32]byte
	ChallengeDigest [32]byte
	TokenKeyID      []byte
	Authenticator   []byte
}
