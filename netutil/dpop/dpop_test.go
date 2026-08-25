// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dpop_test

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/dpop"
)

func TestJWKThumbprint_RFC7638(t *testing.T) {
	t.Parallel()

	// RFC 7638 §3.1 official test vector
	jwk := &dpop.JWK{
		Kty: "RSA",
		N:   "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
		E:   "AQAB",
		Alg: "RS256",
		Kid: "2011-04-29",
	}

	thumbprint, err := dpop.ComputeThumbprint(jwk)
	require.NoError(t, err)

	expected := "NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs"
	assert.Equal(t, expected, thumbprint)
}

func TestDPoP_ECDSAP256(t *testing.T) {
	t.Parallel()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	targetURI := "https://resource.example.com/protected/resource?param=test#frag"
	method := "POST"
	accessToken := "Kz~8mXK1mgalYOPdof0f Nebraska-123"
	nonce := "eyJhbGciOi..."

	proof, err := dpop.CreateProof(priv, method, targetURI, dpop.ProofOptions{
		AccessToken: accessToken,
		Nonce:       nonce,
	})
	require.NoError(t, err)
	require.NotEmpty(t, proof)

	// Verify proof
	claims, jwk, err := dpop.VerifyProof(proof, dpop.VerifierConfig{
		ExpectedMethod: "POST",
		ExpectedURI:    "https://resource.example.com/protected/resource",
		AccessToken:    accessToken,
		ExpectedNonce:  nonce,
	})
	require.NoError(t, err)
	require.NotNil(t, claims)
	require.NotNil(t, jwk)

	assert.Equal(t, "POST", claims.HTM)
	assert.Equal(t, "https://resource.example.com/protected/resource", claims.HTU)
	assert.Equal(t, nonce, claims.Nonce)
	assert.Equal(t, dpop.ComputeAccessTokenHash(accessToken), claims.ATH)

	// Test thumbprint derivation
	jkt, err := dpop.ComputeThumbprint(jwk)
	require.NoError(t, err)
	require.NotEmpty(t, jkt)

	// Method mismatch
	_, _, err = dpop.VerifyProof(proof, dpop.VerifierConfig{
		ExpectedMethod: "GET",
		ExpectedURI:    "https://resource.example.com/protected/resource",
	})
	require.ErrorIs(t, err, dpop.ErrMethodMismatch)

	// URI mismatch
	_, _, err = dpop.VerifyProof(proof, dpop.VerifierConfig{
		ExpectedMethod: "POST",
		ExpectedURI:    "https://other.example.com/resource",
	})
	require.ErrorIs(t, err, dpop.ErrURIMismatch)

	// Access token mismatch
	_, _, err = dpop.VerifyProof(proof, dpop.VerifierConfig{
		ExpectedMethod: "POST",
		ExpectedURI:    "https://resource.example.com/protected/resource",
		AccessToken:    "wrong-access-token",
	})
	require.ErrorIs(t, err, dpop.ErrAccessTokenMismatch)

	// Nonce mismatch
	_, _, err = dpop.VerifyProof(proof, dpop.VerifierConfig{
		ExpectedMethod: "POST",
		ExpectedURI:    "https://resource.example.com/protected/resource",
		ExpectedNonce:  "wrong-nonce",
	})
	require.ErrorIs(t, err, dpop.ErrNonceMismatch)
}

func TestDPoP_Ed25519(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	targetURI := "https://auth.server.com/oauth/token"
	proof, err := dpop.CreateProof(priv, "POST", targetURI)
	require.NoError(t, err)

	claims, jwk, err := dpop.VerifyProof(proof, dpop.VerifierConfig{
		ExpectedMethod: "POST",
		ExpectedURI:    targetURI,
	})
	require.NoError(t, err)
	assert.Equal(t, "OKP", jwk.Kty)
	assert.Equal(t, "Ed25519", jwk.Crv)
	assert.Equal(t, "POST", claims.HTM)

	pubExtracted, err := dpop.JWKToPublicKey(jwk)
	require.NoError(t, err)
	assert.Equal(t, pub, pubExtracted)
}

func TestDPoP_RSA(t *testing.T) {
	t.Parallel()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	targetURI := "https://api.gateway.com/v1/userinfo"
	proof, err := dpop.CreateProof(priv, "GET", targetURI)
	require.NoError(t, err)

	claims, jwk, err := dpop.VerifyProof(proof, dpop.VerifierConfig{
		ExpectedMethod: "GET",
		ExpectedURI:    targetURI,
	})
	require.NoError(t, err)
	assert.Equal(t, "RSA", jwk.Kty)
	assert.Equal(t, "GET", claims.HTM)
}

func TestDPoP_HTTPRequestVerification(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, "https://api.mastodon.social/api/v1/accounts/verify_credentials", nil)
	require.NoError(t, err)

	accessToken := "oauth2-dpop-access-token-xyz"
	proof, err := dpop.CreateProofForRequest(req, priv, dpop.ProofOptions{
		AccessToken: accessToken,
	})
	require.NoError(t, err)

	req.Header.Set(dpop.HeaderAuthorization, dpop.SchemeDPoP+" "+accessToken)
	req.Header.Set(dpop.HeaderDPoP, proof)

	claims, _, err := dpop.VerifyProofForRequest(req, dpop.VerifierConfig{})
	require.NoError(t, err)
	assert.Equal(t, "GET", claims.HTM)
	assert.Equal(t, "https://api.mastodon.social/api/v1/accounts/verify_credentials", claims.HTU)
	assert.Equal(t, dpop.ComputeAccessTokenHash(accessToken), claims.ATH)
}
