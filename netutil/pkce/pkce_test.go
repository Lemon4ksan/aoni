// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pkce_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/netutil/pkce"
)

func TestPKCE_GenerateVerifier_RFC7636(t *testing.T) {
	t.Parallel()

	// Default length (64)
	v1, err := pkce.GenerateVerifier(0)
	require.NoError(t, err)
	assert.Len(t, v1, 64)

	// Explicit valid length (43)
	v2, err := pkce.GenerateVerifier(43)
	require.NoError(t, err)
	assert.Len(t, v2, 43)

	// Explicit valid length (128)
	v3, err := pkce.GenerateVerifier(128)
	require.NoError(t, err)
	assert.Len(t, v3, 128)

	// Length boundaries (<43 or >128)
	_, errLow := pkce.GenerateVerifier(42)
	assert.ErrorIs(t, errLow, pkce.ErrVerifierLength)

	_, errHigh := pkce.GenerateVerifier(129)
	assert.ErrorIs(t, errHigh, pkce.ErrVerifierLength)

	// MustGenerateVerifier
	vMust := pkce.MustGenerateVerifier(64)
	assert.Len(t, vMust, 64)
}

func TestPKCE_ComputeChallenge_RFC7636_AppendixB(t *testing.T) {
	t.Parallel()

	// RFC 7636 Appendix B Official Test Vector
	const (
		verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
		challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	)

	// S256 Method
	ch, err := pkce.ComputeChallenge(verifier, pkce.MethodS256)
	require.NoError(t, err)
	assert.Equal(t, challenge, ch)

	// Default empty method maps to S256
	chDefault, errDefault := pkce.ComputeChallenge(verifier)
	require.NoError(t, errDefault)
	assert.Equal(t, challenge, chDefault)

	// Plain Method
	chPlain, errPlain := pkce.ComputeChallenge(verifier, pkce.MethodPlain)
	require.NoError(t, errPlain)
	assert.Equal(t, verifier, chPlain)

	// Invalid method
	_, errBad := pkce.ComputeChallenge(verifier, "UNKNOWN_METHOD")
	assert.ErrorIs(t, errBad, pkce.ErrInvalidMethod)
}

func TestPKCE_Validate_RFC7636(t *testing.T) {
	t.Parallel()

	const (
		verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
		challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	)

	// Valid S256
	assert.True(t, pkce.Validate(verifier, challenge, pkce.MethodS256))
	assert.True(t, pkce.Validate(verifier, challenge))

	// Invalid verifier
	assert.False(t, pkce.Validate("invalid-verifier-123456789012345678901234567890123", challenge, pkce.MethodS256))

	// Invalid challenge
	assert.False(t, pkce.Validate(verifier, "invalid-challenge", pkce.MethodS256))
}

func TestPKCE_NewPair(t *testing.T) {
	t.Parallel()

	pair, err := pkce.New(64)
	require.NoError(t, err)
	assert.Len(t, pair.Verifier, 64)
	assert.NotEmpty(t, pair.Challenge)
	assert.Equal(t, pkce.MethodS256, pair.Method)
	assert.True(t, pkce.Validate(pair.Verifier, pair.Challenge))
}

func TestPKCE_MatchRedirectURI_RFC9700(t *testing.T) {
	t.Parallel()

	// Exact string match (RFC 9700 §2.1 & §4.1.3)
	assert.True(t, pkce.MatchRedirectURI(
		"https://client.example.com/oauth/callback",
		"https://client.example.com/oauth/callback",
	))

	// Subdomain / wildcard bypass attempt (RFC 9700 §4.1.1)
	assert.False(t, pkce.MatchRedirectURI(
		"https://client.example.com/oauth/callback",
		"https://attacker.example/.client.example.com/oauth/callback",
	))

	// Path mismatch (RFC 9700 §4.1.3)
	assert.False(t, pkce.MatchRedirectURI(
		"https://client.example.com/oauth/callback",
		"https://client.example.com/oauth/callback2",
	))

	// Native app localhost loopback variable port exception (RFC 9700 §2.1 & RFC 8252 §7.3)
	assert.True(t, pkce.MatchRedirectURI(
		"http://127.0.0.1/callback",
		"http://127.0.0.1:8080/callback",
	))
	assert.True(t, pkce.MatchRedirectURI(
		"http://localhost/callback",
		"http://localhost:54321/callback",
	))
	assert.True(t, pkce.MatchRedirectURI(
		"http://[::1]/callback",
		"http://[::1]:9090/callback",
	))

	// Localhost loopback path mismatch still rejected
	assert.False(t, pkce.MatchRedirectURI(
		"http://localhost/callback",
		"http://localhost:54321/different_path",
	))

	// Empty inputs
	assert.False(t, pkce.MatchRedirectURI("", "https://example.com"))
	assert.False(t, pkce.MatchRedirectURI("https://example.com", ""))
}
