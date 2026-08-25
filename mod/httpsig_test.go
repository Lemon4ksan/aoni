// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"testing"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/netutil/httpsig"
)

func TestWithHTTPSignature(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	req := newDummyRequest()
	req.SetMethod("POST")
	req.SetURL("https://api.gateway.com/v1/orders")
	req.SetHeader("Content-Type", "application/json")
	req.SetBodyBytes([]byte(`{"order_id":"123","total":100}`))

	// Apply Content-Digest modifier
	digestMod := mod.WithContentDigest(httpsig.DigestSHA256)
	digestMod.Apply(req)

	digestVal := req.Header(httpsig.HeaderContentDigest)
	require.NotEmpty(t, digestVal)
	assert.Contains(t, digestVal, "sha-256=:")

	// Apply HTTP Message Signature modifier
	sigMod := mod.WithHTTPSignatureKey(
		"key-1",
		priv,
		"@method",
		"@authority",
		"@path",
		"content-type",
		"content-digest",
	)
	sigMod.Apply(req)

	sigInput := req.Header(httpsig.HeaderSignatureInput)
	sig := req.Header(httpsig.HeaderSignature)

	require.NotEmpty(t, sigInput)
	require.NotEmpty(t, sig)
	assert.Contains(t, sigInput, `sig1=("@method" "@authority" "@path" "content-type" "content-digest")`)

	// Verify using netutil/httpsig verifier
	verifier, err := httpsig.NewEd25519Verifier("key-1", pub)
	require.NoError(t, err)

	stdReq, err := http.NewRequest("POST", "https://api.gateway.com/v1/orders", nil)
	require.NoError(t, err)
	stdReq.Header.Set("Content-Type", "application/json")
	stdReq.Header.Set(httpsig.HeaderContentDigest, digestVal)
	stdReq.Header.Set(httpsig.HeaderSignatureInput, sigInput)
	stdReq.Header.Set(httpsig.HeaderSignature, sig)

	params, err := httpsig.VerifyRequest(stdReq, httpsig.VerifyConfig{
		Verifier: verifier,
		RequiredComponents: []string{
			"@method",
			"@authority",
			"@path",
			"content-type",
			"content-digest",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, params)
	assert.Equal(t, "key-1", params.KeyID)
}
