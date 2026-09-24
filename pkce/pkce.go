// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package pkce implements Proof Key for Code Exchange strictly conforming to RFC 7636 and OAuth 2.0 Security BCP (RFC 9700).
package pkce

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// RFC 7636 §4.2 & RFC 9700 §2.1: PKCE Code Challenge Methods.
const (
	// MethodS256 represents the SHA-256 code challenge transformation (RFC 7636 §4.2).
	// RFC 9700 §2.1 mandates/recommends S256 to prevent code verifier exposure in authorization requests.
	MethodS256 = "S256"

	// MethodPlain represents the identity transformation (RFC 7636 §4.2).
	// Deprecated and discouraged by RFC 9700 §2.1 because plain verifiers leak to network/web observers.
	MethodPlain = "plain"
)

var (
	// ErrVerifierLength is returned when a PKCE code verifier is shorter than 43 or longer than 128 chars (RFC 7636 §4.1).
	ErrVerifierLength = errors.New("foundation/pkce: code verifier length must be between 43 and 128 characters")

	// ErrInvalidMethod is returned when an unsupported code challenge method is requested.
	ErrInvalidMethod = errors.New("foundation/pkce: unsupported code challenge method (use S256)")
)

// ChallengePair represents a generated PKCE code verifier and its corresponding code challenge.
type ChallengePair struct {
	Verifier  string
	Challenge string
	Method    string
}

// New creates a new [ChallengePair] with a cryptographically secure verifier of default length 64 (or custom length).
func New(length ...int) (*ChallengePair, error) {
	l := 64
	if len(length) > 0 && length[0] > 0 {
		l = length[0]
	}

	verifier, err := GenerateVerifier(l)
	if err != nil {
		return nil, err
	}

	challenge, err := ComputeChallenge(verifier, MethodS256)
	if err != nil {
		return nil, err
	}

	return &ChallengePair{
		Verifier:  verifier,
		Challenge: challenge,
		Method:    MethodS256,
	}, nil
}

// GenerateVerifier creates a cryptographically secure, high-entropy PKCE code verifier
// conforming to RFC 7636 §4.1 with length between 43 and 128 characters (default 64 if length <= 0).
func GenerateVerifier(length int) (string, error) {
	if length <= 0 {
		length = 64
	}

	if length < 43 || length > 128 {
		return "", ErrVerifierLength
	}

	// Calculate raw byte count needed for base64url encoding
	rawLen := (length * 3) / 4
	if (length*3)%4 != 0 {
		rawLen++
	}

	buf := make([]byte, rawLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("foundation/pkce: failed to generate random bytes: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(buf)
	if len(encoded) > length {
		encoded = encoded[:length]
	}

	return encoded, nil
}

// MustGenerateVerifier generates a PKCE code verifier, panicking on entropy failure.
func MustGenerateVerifier(length int) string {
	v, err := GenerateVerifier(length)
	if err != nil {
		panic(err)
	}

	return v
}

// ComputeChallenge calculates the PKCE code_challenge from a code_verifier
// using the specified transformation method (RFC 7636 §4.2, RFC 9700 §2.1).
func ComputeChallenge(verifier string, method ...string) (string, error) {
	if len(verifier) < 43 || len(verifier) > 128 {
		return "", ErrVerifierLength
	}

	m := MethodS256
	if len(method) > 0 && method[0] != "" {
		m = method[0]
	}

	switch m {
	case MethodS256:
		h := sha256.Sum256([]byte(verifier))
		return base64.RawURLEncoding.EncodeToString(h[:]), nil

	case MethodPlain:
		return verifier, nil

	default:
		return "", ErrInvalidMethod
	}
}

// Validate performs constant-time validation of a code_verifier against a code_challenge (RFC 7636 §4.6).
func Validate(verifier, challenge string, method ...string) bool {
	m := MethodS256
	if len(method) > 0 && method[0] != "" {
		m = method[0]
	}

	computed, err := ComputeChallenge(verifier, m)
	if err != nil {
		return false
	}

	if len(computed) != len(challenge) {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

// MatchRedirectURI verifies whether requestURI matches registeredURI according to RFC 9700 §2.1 & §4.1.3:
//   - Exact string comparison is enforced (RFC 3986 §6.2.1)
//   - Wildcards / pattern matching are strictly prohibited to prevent credential exfiltration (RFC 9700 §4.1)
//   - Variable port numbers are permitted only for native apps using localhost loopback (RFC 8252 §7.3, RFC 9700 §2.1)
func MatchRedirectURI(registeredURI, requestURI string) bool {
	if registeredURI == "" || requestURI == "" {
		return false
	}

	// Fast path: exact string identity (RFC 3986 §6.2.1 simple string comparison)
	if registeredURI == requestURI {
		return true
	}

	// Parse both URIs to evaluate localhost variable port exception
	regURL, errReg := url.Parse(registeredURI)

	reqURL, errReq := url.Parse(requestURI)
	if errReg != nil || errReq != nil {
		return false
	}

	// RFC 9700 §2.1 & RFC 8252 §7.3: Localhost loopback variable port exception for native apps
	if isLoopbackHost(regURL.Hostname()) && isLoopbackHost(reqURL.Hostname()) {
		// Schemes, paths, queries, and fragments must match exactly; only the port may vary
		if strings.EqualFold(regURL.Scheme, reqURL.Scheme) &&
			regURL.Path == reqURL.Path &&
			regURL.RawQuery == reqURL.RawQuery &&
			regURL.Fragment == reqURL.Fragment {
			return true
		}
	}

	return false
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}
