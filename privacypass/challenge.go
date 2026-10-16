// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package privacypass

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// AuthChallenge represents a single parsed HTTP authentication challenge from a WWW-Authenticate header.
//
// Invariants:
//   - Scheme contains the authentication scheme token (e.g. "PrivateToken", "Basic", "Bearer").
//   - Params contains parsed key-value parameters normalized to lowercase keys.
type AuthChallenge struct {
	// Scheme is the authentication scheme token.
	Scheme string

	// Params contains the challenge parameters with lowercase keys.
	Params map[string]string

	// Raw is the original header string from which this challenge was parsed.
	Raw string
}

// ParseChallenge parses the first valid PrivateToken challenge from a WWW-Authenticate header string.
//
// It locates the first challenge with scheme "PrivateToken" (case-insensitive), extracts its
// mandatory 'challenge' base64url parameter, decodes and unpacks the binary [TokenChallenge] structure,
// computes its SHA-256 digest, and extracts optional parameters ('token-key', 'max-age', 'token-type').
//
// Invariants:
//   - Returns [ErrNoChallenge] if header is empty or contains only whitespace.
//   - Returns [ErrNoPrivateTokenChallenge] if no PrivateToken scheme is present.
//   - Returns [ErrMissingChallengeParam] if the 'challenge' attribute is missing.
//   - Returns [ErrTruncatedChallenge] if the 'challenge' attribute is empty.
//   - Returns [ErrInvalidEncoding] if base64url decoding fails.
//   - Panic-safe on all pathological inputs.
func ParseChallenge(header string) (*TokenChallenge, error) {
	challenges, err := ParseChallenges(header)
	if err != nil {
		return nil, err
	}

	if len(challenges) == 0 {
		return nil, ErrNoPrivateTokenChallenge
	}

	return challenges[0], nil
}

// ParseChallenges parses all PrivateToken challenges from a WWW-Authenticate header string.
//
// When an Origin offers multiple PrivateToken challenges (e.g., both Type 2 Blind RSA and
// Type 1 VOPRF), ParseChallenges unpacks all valid PrivateToken challenges present in order.
//
// Invariants:
//   - Resolves RFC 9110 comma-overloading (discriminates auth-params vs multiple challenge schemes).
//   - Case-insensitive scheme and parameter matching.
//   - Preserves order of challenges in the header.
func ParseChallenges(header string) ([]*TokenChallenge, error) {
	header = strings.TrimSpace(header)
	if len(header) == 0 {
		return nil, ErrNoChallenge
	}

	authChallenges, err := parseAuthChallenges(header)
	if err != nil {
		return nil, err
	}

	var (
		parsedChallenges []*TokenChallenge
		firstErr         error
	)

	for _, ac := range authChallenges {
		if !equalFoldASCII(ac.Scheme, "PrivateToken") {
			continue
		}

		rawB64Challenge, ok := ac.Params["challenge"]
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf(
					"%w: PrivateToken scheme found without 'challenge' parameter",
					ErrMissingChallengeParam,
				)
			}

			continue
		}

		if len(rawB64Challenge) == 0 {
			if firstErr == nil {
				firstErr = fmt.Errorf("%w: challenge parameter is empty", ErrTruncatedChallenge)
			}

			continue
		}

		rawBytes, err := DecodeBase64URL(rawB64Challenge)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%w: invalid challenge base64url: %w", ErrInvalidEncoding, err)
			}

			continue
		}

		tc, err := UnmarshalTokenChallenge(rawBytes)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%w: %w", ErrMalformedChallenge, err)
			}

			continue
		}

		// Parse optional token-key (SPKI)
		if keyB64, ok := ac.Params["token-key"]; ok && len(keyB64) > 0 {
			if keyBytes, err := DecodeBase64URL(keyB64); err == nil && len(keyBytes) > 0 {
				tc.TokenKey = keyBytes
				tc.TokenKeyID = sha256.Sum256(keyBytes)
			}
		}

		// Parse optional token-key-id fallback if token-key was not provided
		if len(tc.TokenKey) == 0 {
			if keyIDB64, ok := ac.Params["token-key-id"]; ok && len(keyIDB64) > 0 {
				if keyIDBytes, err := DecodeBase64URL(keyIDB64); err == nil && len(keyIDBytes) == TokenKeyIDSize {
					copy(tc.TokenKeyID[:], keyIDBytes)
				}
			}
		}

		// Parse optional max-age
		if maxAgeStr, ok := ac.Params["max-age"]; ok && len(maxAgeStr) > 0 {
			if sec, err := strconv.Atoi(maxAgeStr); err == nil && sec > 0 {
				tc.MaxAge = time.Duration(sec) * time.Second
			}
		}

		// Validate optional token-type hint if present
		if tokenTypeStr, ok := ac.Params["token-type"]; ok && len(tokenTypeStr) > 0 {
			if tt, err := strconv.ParseUint(tokenTypeStr, 10, 16); err == nil {
				if uint16(tt) != tc.TokenType {
					if firstErr == nil {
						firstErr = fmt.Errorf(
							"%w: parameter token-type (%d) does not match challenge token_type (%d)",
							ErrMalformedChallenge,
							tt,
							tc.TokenType,
						)
					}

					continue
				}
			}
		}

		parsedChallenges = append(parsedChallenges, tc)
	}

	if len(parsedChallenges) > 0 {
		return parsedChallenges, nil
	}

	if firstErr != nil {
		return nil, firstErr
	}

	return nil, ErrNoPrivateTokenChallenge
}

// ParseChallengesFromHeader inspects all "WWW-Authenticate" entries in an [http.Header] map
// and extracts all valid PrivateToken challenges.
//
// Invariants:
//   - Returns [ErrNoChallenge] if header map is nil, empty, or has no WWW-Authenticate headers.
//   - Returns [ErrNoPrivateTokenChallenge] if no PrivateToken challenges are found.
func ParseChallengesFromHeader(h http.Header) ([]*TokenChallenge, error) {
	if len(h) == 0 {
		return nil, ErrNoChallenge
	}

	vals := h.Values("WWW-Authenticate")
	if len(vals) == 0 {
		return nil, ErrNoChallenge
	}

	var (
		all      []*TokenChallenge
		firstErr error
	)

	for _, val := range vals {
		challenges, err := ParseChallenges(val)
		if err != nil {
			if firstErr == nil && !errors.Is(err, ErrNoPrivateTokenChallenge) {
				firstErr = err
			}

			continue
		}

		all = append(all, challenges...)
	}

	if len(all) > 0 {
		return all, nil
	}

	if firstErr != nil {
		return nil, firstErr
	}

	return nil, ErrNoPrivateTokenChallenge
}

// FormatAuthorization formats an Authorization header value containing an unblinded Token
// per RFC 9577 §2.2:
//
//	Authorization: PrivateToken token="<base64url>"
//
// If token is nil or fails serialization, an empty string is returned.
func FormatAuthorization(token *Token) string {
	res, _ := FormatAuthorizationStrict(token)
	return res
}

// FormatAuthorizationStrict formats an Authorization header value containing an unblinded Token,
// returning an explicit error if token is nil, malformed, or has an empty authenticator.
//
// Invariants:
//   - Returns [ErrNilToken] if token is nil.
//   - Returns [ErrEmptyAuthenticator] if token.Authenticator is empty.
func FormatAuthorizationStrict(token *Token) (string, error) {
	if token == nil {
		return "", ErrNilToken
	}

	raw, err := MarshalToken(token)
	if err != nil {
		return "", err
	}

	return FormatAuthorizationBytes(raw), nil
}

// FormatAuthorizationBytes formats an Authorization header value directly from raw binary Token bytes.
//
// Invariants:
//   - Emits RFC 4648 §5 unpadded base64url encoding.
//   - Pre-calculates exact buffer capacity, incurring exactly 1 heap allocation for the returned string.
func FormatAuthorizationBytes(rawToken []byte) string {
	const prefix = `PrivateToken token="`

	b64Len := base64.RawURLEncoding.EncodedLen(len(rawToken))
	totalLen := len(prefix) + b64Len + 1

	buf := make([]byte, totalLen)
	copy(buf, prefix)
	base64.RawURLEncoding.Encode(buf[len(prefix):], rawToken)
	buf[totalLen-1] = '"'

	return string(buf)
}

// ParseAuthorization parses an Authorization header value formatted per RFC 9577 §2.2
// and extracts the unblinded [Token] structure.
//
// It supports both standard syntax (token="<base64url>") and unquoted token68 syntax.
//
// Invariants:
//   - Returns [ErrMalformedAuthorization] if scheme is not PrivateToken or syntax is invalid.
//   - Returns [ErrMissingTokenParam] if token parameter is absent.
func ParseAuthorization(header string) (*Token, error) {
	header = strings.TrimSpace(header)
	if len(header) == 0 {
		return nil, ErrMalformedAuthorization
	}

	parts := strings.SplitN(header, " ", 2)
	if !equalFoldASCII(parts[0], "PrivateToken") {
		return nil, fmt.Errorf("%w: scheme must be PrivateToken", ErrMalformedAuthorization)
	}

	if len(parts) < 2 {
		return nil, ErrMissingTokenParam
	}

	rest := strings.TrimSpace(parts[1])

	var tokenB64 string

	if len(rest) >= 6 && equalFoldASCII(rest[:6], "token=") {
		val := strings.TrimSpace(rest[6:])
		if len(val) > 0 && val[0] == '"' {
			endQuote := strings.IndexByte(val[1:], '"')
			if endQuote >= 0 {
				tokenB64 = val[1 : 1+endQuote]
			} else {
				tokenB64 = val[1:]
			}
		} else {
			end := strings.IndexAny(val, " ,;\t\r\n")
			if end >= 0 {
				tokenB64 = val[:end]
			} else {
				tokenB64 = val
			}
		}
	} else if len(rest) > 0 && rest[0] != '=' {
		// Fallback for token68 syntax
		tokenB64 = rest
	}

	if len(tokenB64) == 0 {
		return nil, ErrMissingTokenParam
	}

	rawBytes, err := DecodeBase64URL(tokenB64)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidEncoding, err)
	}

	token, err := UnmarshalToken(rawBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedAuthorization, err)
	}

	return token, nil
}

// IsPrivateTokenChallenge performs a zero-allocation fast-path check to determine whether
// a WWW-Authenticate header string contains a PrivateToken scheme.
//
// Invariants:
//   - Zero heap allocations.
//   - Case-insensitive ASCII comparison.
func IsPrivateTokenChallenge(header string) bool {
	headerLen := len(header)

	const target = "privatetoken"

	targetLen := len(target)

	for i := 0; i <= headerLen-targetLen; i++ {
		if (i == 0 || isSeparator(header[i-1])) && equalFoldASCII(header[i:i+targetLen], target) {
			end := i + targetLen
			if end == headerLen || isSeparator(header[end]) {
				return true
			}
		}
	}

	return false
}

// HasPrivateTokenChallenge performs a fast-path check on an [http.Header] map.
//
// Invariants:
//   - Returns true if any WWW-Authenticate header contains a PrivateToken challenge.
func HasPrivateTokenChallenge(h http.Header) bool {
	if len(h) == 0 {
		return false
	}

	for _, val := range h.Values("WWW-Authenticate") {
		if IsPrivateTokenChallenge(val) {
			return true
		}
	}

	return false
}

// EncodeBase64URL encodes data into unpadded URL-safe base64 per RFC 9577 and RFC 4648 §5.
//
// Invariants:
//   - Emits unpadded URL-safe alphabet ('-' and '_', no '=').
func EncodeBase64URL(src []byte) string {
	return base64.RawURLEncoding.EncodeToString(src)
}

// DecodeBase64URL decodes an unpadded URL-safe base64 string per RFC 9577, with graceful
// fallback tolerance for padded base64url and standard base64 encodings.
//
// Invariants:
//   - 4-tier fallback: RawURLEncoding -> URLEncoding -> RawStdEncoding -> StdEncoding.
//   - Returns [ErrInvalidEncoding] if all decoding attempts fail.
func DecodeBase64URL(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return nil, fmt.Errorf("%w: empty base64 string", ErrInvalidEncoding)
	}

	// 1. RFC 9577 canonical: unpadded URL-safe
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}

	// 2. Fallback: padded URL-safe
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}

	// 3. Fallback: unpadded standard base64
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}

	// 4. Fallback: padded standard base64
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}

	return nil, fmt.Errorf("%w: failed to decode base64 string", ErrInvalidEncoding)
}

// -----------------------------------------------------------------------------
// Internal Zero-Allocation RFC 9110 Parsing Engine
// -----------------------------------------------------------------------------

func parseAuthChallenges(header string) ([]*AuthChallenge, error) {
	var challenges []*AuthChallenge

	sc := &authScanner{s: header, pos: 0}

	var current *AuthChallenge

	for {
		tok := sc.nextToken()
		if tok.kind == tokEOF {
			break
		}

		if tok.kind == tokComma {
			continue
		}

		if tok.kind != tokWord {
			continue
		}

		if sc.isNextEquals() {
			// This word is a parameter name
			key := tok.text

			sc.consumeEquals()

			valTok := sc.nextValue()
			if current != nil {
				current.Params[strings.ToLower(key)] = valTok.text
			}
		} else {
			// This word is an auth-scheme or token68
			if current == nil || len(current.Params) > 0 || sc.nextWordIsFollowedByEquals() {
				current = &AuthChallenge{
					Scheme: tok.text,
					Params: make(map[string]string),
					Raw:    header,
				}
				challenges = append(challenges, current)
			} else {
				current.Params["token68"] = tok.text
			}
		}
	}

	return challenges, nil
}

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokWord
	tokEquals
	tokComma
	tokQuoted
)

type scanToken struct {
	kind tokenKind
	text string
}

type authScanner struct {
	s   string
	pos int
}

func (sc *authScanner) nextToken() scanToken {
	sc.skipWhitespace()

	if sc.pos >= len(sc.s) {
		return scanToken{kind: tokEOF}
	}

	c := sc.s[sc.pos]
	if c == ',' {
		sc.pos++
		return scanToken{kind: tokComma, text: ","}
	}

	if c == '=' {
		sc.pos++
		return scanToken{kind: tokEquals, text: "="}
	}

	if c == '"' {
		return sc.scanQuoted()
	}

	start := sc.pos
	for sc.pos < len(sc.s) {
		ch := sc.s[sc.pos]
		if ch == ',' || ch == '=' || ch == '"' || isWhitespace(ch) {
			break
		}

		sc.pos++
	}

	return scanToken{kind: tokWord, text: sc.s[start:sc.pos]}
}

func (sc *authScanner) isNextEquals() bool {
	saved := sc.pos
	defer func() { sc.pos = saved }()

	sc.skipWhitespace()

	return sc.pos < len(sc.s) && sc.s[sc.pos] == '='
}

func (sc *authScanner) consumeEquals() {
	sc.skipWhitespace()

	if sc.pos < len(sc.s) && sc.s[sc.pos] == '=' {
		sc.pos++
	}
}

func (sc *authScanner) nextWordIsFollowedByEquals() bool {
	saved := sc.pos
	defer func() { sc.pos = saved }()

	sc.skipWhitespace()

	for sc.pos < len(sc.s) && !isSeparator(sc.s[sc.pos]) {
		sc.pos++
	}

	sc.skipWhitespace()

	return sc.pos < len(sc.s) && sc.s[sc.pos] == '='
}

func (sc *authScanner) nextValue() scanToken {
	sc.skipWhitespace()

	if sc.pos >= len(sc.s) {
		return scanToken{kind: tokWord, text: ""}
	}

	if sc.s[sc.pos] == '"' {
		return sc.scanQuoted()
	}

	start := sc.pos
	for sc.pos < len(sc.s) {
		ch := sc.s[sc.pos]
		if ch == ',' || isWhitespace(ch) {
			break
		}

		sc.pos++
	}

	return scanToken{kind: tokWord, text: sc.s[start:sc.pos]}
}

func (sc *authScanner) scanQuoted() scanToken {
	sc.pos++ // consume opening quote
	start := sc.pos
	hasEscapes := false

	for sc.pos < len(sc.s) {
		ch := sc.s[sc.pos]
		if ch == '\\' {
			hasEscapes = true

			if sc.pos+1 < len(sc.s) {
				sc.pos += 2 // skip backslash and escaped char
			} else {
				sc.pos++ // trailing backslash before EOF
			}

			continue
		}

		if ch == '"' {
			val := sc.s[start:sc.pos]
			sc.pos++ // consume closing quote

			if hasEscapes {
				return scanToken{kind: tokQuoted, text: unescapeQuotedString(val)}
			}

			return scanToken{kind: tokQuoted, text: val}
		}

		sc.pos++
	}

	// Missing closing quote: gracefully consume to EOF
	if sc.pos > len(sc.s) {
		sc.pos = len(sc.s)
	}

	val := sc.s[start:sc.pos]
	if hasEscapes {
		return scanToken{kind: tokQuoted, text: unescapeQuotedString(val)}
	}

	return scanToken{kind: tokQuoted, text: val}
}

func (sc *authScanner) skipWhitespace() {
	for sc.pos < len(sc.s) && isWhitespace(sc.s[sc.pos]) {
		sc.pos++
	}
}

func isWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func isSeparator(c byte) bool {
	return isWhitespace(c) || c == ',' || c == ';' || c == '='
}

func unescapeQuotedString(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}

		sb.WriteByte(s[i])
	}

	return sb.String()
}

func equalFoldASCII(s, target string) bool {
	if len(s) != len(target) {
		return false
	}

	for i := 0; i < len(s); i++ {
		c1 := s[i]
		c2 := target[i]

		if c1 >= 'A' && c1 <= 'Z' {
			c1 += 'a' - 'A'
		}

		if c2 >= 'A' && c2 <= 'Z' {
			c2 += 'a' - 'A'
		}

		if c1 != c2 {
			return false
		}
	}

	return true
}
