// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil/dpop"
)

func TestWithDPoP(t *testing.T) {
	t.Parallel()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	req := newDummyRequest()
	req.SetMethod("POST")
	req.SetURL("https://oauth.provider.com/token")

	dpopMod := mod.WithDPoP(priv)
	dpopMod.Apply(req)

	proof := req.Header(dpop.HeaderDPoP)
	require.NotEmpty(t, proof)

	claims, jwk, err := dpop.VerifyProof(proof, dpop.VerifierConfig{
		ExpectedMethod: "POST",
		ExpectedURI:    "https://oauth.provider.com/token",
	})
	require.NoError(t, err)
	require.NotNil(t, claims)
	require.NotNil(t, jwk)
	assert.Equal(t, "POST", claims.HTM)
	assert.Equal(t, "https://oauth.provider.com/token", claims.HTU)
}

func TestWithDPoPToken(t *testing.T) {
	t.Parallel()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	req := newDummyRequest()
	req.SetMethod("GET")
	req.SetURL("https://api.service.com/v1/profile?detail=true")

	accessToken := "sample-dpop-access-token-987"
	dpopTokenMod := mod.WithDPoPToken(accessToken, priv)
	dpopTokenMod.Apply(req)

	authVal := req.Header(dpop.HeaderAuthorization)
	require.Equal(t, "DPoP "+accessToken, authVal)

	proof := req.Header(dpop.HeaderDPoP)
	require.NotEmpty(t, proof)

	stdReq, err := http.NewRequest("GET", "https://api.service.com/v1/profile?detail=true", nil)
	require.NoError(t, err)
	stdReq.Header.Set(dpop.HeaderAuthorization, authVal)
	stdReq.Header.Set(dpop.HeaderDPoP, proof)

	claims, _, err := dpop.VerifyProofForRequest(stdReq, dpop.VerifierConfig{})
	require.NoError(t, err)
	require.NotNil(t, claims)
	assert.Equal(t, "GET", claims.HTM)
	assert.Equal(t, "https://api.service.com/v1/profile", claims.HTU)
	assert.Equal(t, dpop.ComputeAccessTokenHash(accessToken), claims.ATH)
}

func TestWithDPoPProof(t *testing.T) {
	t.Parallel()

	req := newDummyRequest()
	mod.WithDPoPProof("raw.jwt.token").Apply(req)

	assert.Equal(t, "raw.jwt.token", req.Header(dpop.HeaderDPoP))
}
