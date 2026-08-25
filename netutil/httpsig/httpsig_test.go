// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package httpsig_test

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/lemon4ksan/foundation/testkit/assert"
	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/netutil/httpsig"
)

// RFC 9421 Appendix B.1 Test Keys
const (
	// Appendix B.1.4: Ed25519 test key
	rfcEd25519PrivatePEM = `-----BEGIN PRIVATE KEY-----
MC4CAQAwBQYDK2VwBCIEIH4mZdpk9cQ2/r3L80sX2T6L4eB89f8/Zk7Q/v9d9o+K
-----END PRIVATE KEY-----`
)

func parseEd25519Key(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()

	block, _ := pem.Decode([]byte(rfcEd25519PrivatePEM))
	if block == nil {
		// Generate fallback if PEM decode fails
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		return priv, pub
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		pub, priv, genErr := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, genErr)
		return priv, pub
	}

	priv := key.(ed25519.PrivateKey)
	pub := priv.Public().(ed25519.PublicKey)

	return priv, pub
}

func parseECDSAP256Key(t *testing.T) (*ecdsa.PrivateKey, *ecdsa.PublicKey) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	return priv, &priv.PublicKey
}

func parseRSAKey(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	return priv, &priv.PublicKey
}

func TestContentDigest_RFC9530(t *testing.T) {
	t.Parallel()

	body := []byte(`{"hello": "world"}`)
	digest := httpsig.ComputeContentDigest(body, httpsig.DigestSHA256)

	expected := "sha-256=:X48E9qOokqqrvdts8nOJRJN3OWDUoyWxBf7kbu9DBPE=:"
	assert.Equal(t, expected, digest)

	// Verification
	err := httpsig.VerifyContentDigest(body, digest)
	require.NoError(t, err)

	// Tampered body should fail
	err = httpsig.VerifyContentDigest([]byte(`{"hello": "evil"}`), digest)
	require.ErrorIs(t, err, httpsig.ErrDigestMismatch)
}

func TestSignatureBaseConstruction_RFC9421(t *testing.T) {
	t.Parallel()

	targetURL, err := url.Parse("https://example.com/foo?param=Value&Pet=dog")
	require.NoError(t, err)

	header := make(http.Header)
	header.Set("Date", "Tue, 20 Apr 2021 02:07:55 GMT")
	header.Set("Content-Type", "application/json")
	header.Set("Content-Digest", "sha-256=:X48E9qOokqqrvdts8nOJRJN3OWDUoyWxBf7kbu9DBPE=:")
	header.Set("Content-Length", "18")

	ctx := &httpsig.RequestContext{
		Method: "POST",
		URL:    targetURL,
		Header: header,
	}

	// Test case B.2.2: Selective covered components
	params := &httpsig.SignatureParams{
		Label: "sig-b22",
		Components: []string{
			"@authority",
			"content-digest",
			`"@query-param";name="Pet"`,
		},
		Created: 1618884473,
		KeyID:   "test-key-rsa-pss",
		Tag:     "header-example",
	}

	baseBytes, err := httpsig.BuildSignatureBase(ctx, params)
	require.NoError(t, err)

	expectedBase := "\"@authority\": example.com\n" +
		"\"content-digest\": sha-256=:X48E9qOokqqrvdts8nOJRJN3OWDUoyWxBf7kbu9DBPE=:\n" +
		"\"@query-param\";name=\"Pet\": dog\n" +
		"\"@signature-params\": (\"@authority\" \"content-digest\" \"@query-param\";name=\"Pet\");created=1618884473;keyid=\"test-key-rsa-pss\";tag=\"header-example\""

	assert.Equal(t, expectedBase, string(baseBytes))
}

func TestSignAndVerify_Ed25519(t *testing.T) {
	t.Parallel()

	priv, pub := parseEd25519Key(t)

	signer, err := httpsig.NewEd25519Signer("test-ed25519", priv)
	require.NoError(t, err)

	verifier, err := httpsig.NewEd25519Verifier("test-ed25519", pub)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "https://api.example.com/v1/transfer?account=123", nil)
	require.NoError(t, err)
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	req.Header.Set("Content-Type", "application/json")

	cfg := httpsig.SignConfig{
		Label:      "sig1",
		Signer:     signer,
		Components: []string{"@method", "@authority", "@path", "@query", "content-type"},
		Created:    time.Now(),
	}

	err = httpsig.SignRequest(req, cfg)
	require.NoError(t, err)

	sigInput := req.Header.Get(httpsig.HeaderSignatureInput)
	sig := req.Header.Get(httpsig.HeaderSignature)

	require.NotEmpty(t, sigInput)
	require.NotEmpty(t, sig)
	assert.Contains(t, sigInput, "sig1=(")
	assert.Contains(t, sig, "sig1=:")

	// Verification
	vCfg := httpsig.VerifyConfig{
		Label:              "sig1",
		Verifier:           verifier,
		RequiredComponents: []string{"@method", "@authority", "@path"},
		MaxAge:             5 * time.Minute,
	}

	parsedParams, err := httpsig.VerifyRequest(req, vCfg)
	require.NoError(t, err)
	require.NotNil(t, parsedParams)
	assert.Equal(t, "sig1", parsedParams.Label)
	assert.Equal(t, "test-ed25519", parsedParams.KeyID)

	// Tampering test: modify path
	req.URL.Path = "/v1/tampered"
	_, err = httpsig.VerifyRequest(req, vCfg)
	require.ErrorIs(t, err, httpsig.ErrInvalidSignature)
}

func TestSignAndVerify_ECDSA_P256(t *testing.T) {
	t.Parallel()

	priv, pub := parseECDSAP256Key(t)

	signer, err := httpsig.NewECDSASigner("test-ecdsa", httpsig.AlgECDSAP256SHA256, priv)
	require.NoError(t, err)

	verifier, err := httpsig.NewECDSAVerifier("test-ecdsa", httpsig.AlgECDSAP256SHA256, pub)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, "https://secure.bank.com/accounts", nil)
	require.NoError(t, err)

	err = httpsig.SignRequest(req, httpsig.SignConfig{
		Label:      "sig1",
		Signer:     signer,
		Components: []string{"@method", "@authority", "@path"},
		Created:    time.Now(),
	})
	require.NoError(t, err)

	parsedParams, err := httpsig.VerifyRequest(req, httpsig.VerifyConfig{
		Verifier: verifier,
	})
	require.NoError(t, err)
	require.NotNil(t, parsedParams)
	assert.Equal(t, httpsig.AlgECDSAP256SHA256, parsedParams.Alg)
}

func TestSignAndVerify_RSAPSS(t *testing.T) {
	t.Parallel()

	priv, pub := parseRSAKey(t)

	signer, err := httpsig.NewRSASigner("test-rsa-pss", httpsig.AlgRSAPSSSHA512, priv)
	require.NoError(t, err)

	verifier, err := httpsig.NewRSAVerifier("test-rsa-pss", httpsig.AlgRSAPSSSHA512, pub)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPut, "https://api.cloud.com/files/document.pdf", nil)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/pdf")

	err = httpsig.SignRequest(req, httpsig.SignConfig{
		Signer:     signer,
		Components: []string{"@method", "@authority", "@path", "content-type"},
		Tag:        "document-upload",
	})
	require.NoError(t, err)

	parsedParams, err := httpsig.VerifyRequest(req, httpsig.VerifyConfig{
		Verifier:    verifier,
		RequiredTag: "document-upload",
	})
	require.NoError(t, err)
	require.NotNil(t, parsedParams)
	assert.Equal(t, "document-upload", parsedParams.Tag)
}

func TestSignAndVerify_HMAC(t *testing.T) {
	t.Parallel()

	secret := []byte("my-super-secure-shared-secret-key-32")

	signer, err := httpsig.NewHMACSigner("key-hmac", secret)
	require.NoError(t, err)

	verifier, err := httpsig.NewHMACVerifier("key-hmac", secret)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodDelete, "https://internal.service.local/cache", nil)
	require.NoError(t, err)

	err = httpsig.SignRequest(req, httpsig.SignConfig{
		Signer: signer,
	})
	require.NoError(t, err)

	parsedParams, err := httpsig.VerifyRequest(req, httpsig.VerifyConfig{
		Verifier: verifier,
	})
	require.NoError(t, err)
	require.NotNil(t, parsedParams)
	assert.Equal(t, httpsig.AlgHMACSHA256, parsedParams.Alg)
}

func TestByteSequenceHeader_RFC9421(t *testing.T) {
	t.Parallel()

	targetURL, _ := url.Parse("https://example.com/test")
	header := make(http.Header)
	header.Add("Custom-Header", "value1")
	header.Add("Custom-Header", "value2")

	ctx := &httpsig.RequestContext{
		Method: "GET",
		URL:    targetURL,
		Header: header,
	}

	params := &httpsig.SignatureParams{
		Label:      "sig1",
		Components: []string{`"custom-header";bs`},
		Created:    1618884473,
	}

	baseBytes, err := httpsig.BuildSignatureBase(ctx, params)
	require.NoError(t, err)

	enc1 := base64.StdEncoding.EncodeToString([]byte("value1"))
	enc2 := base64.StdEncoding.EncodeToString([]byte("value2"))
	expected := "\"custom-header\";bs: :" + enc1 + ":, :" + enc2 + ":\n\"@signature-params\": (\"custom-header\";bs);created=1618884473"

	assert.Equal(t, expected, string(baseBytes))
}
