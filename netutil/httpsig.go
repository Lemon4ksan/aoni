// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netutil

import (
	"net/http"

	"github.com/lemon4ksan/aoni/netutil/httpsig"
)

// HTTP Message Signatures (RFC 9421) and Digest Fields (RFC 9530) type aliases.
type (
	// HTTPSigAlgorithm specifies an RFC 9421 cryptographic signature algorithm.
	HTTPSigAlgorithm = httpsig.Algorithm

	// HTTPSigSigner signs HTTP message components according to RFC 9421.
	HTTPSigSigner = httpsig.Signer

	// HTTPSigVerifier verifies HTTP message signatures according to RFC 9421.
	HTTPSigVerifier = httpsig.Verifier

	// HTTPSigSignConfig holds options for creating an RFC 9421 signature.
	HTTPSigSignConfig = httpsig.SignConfig

	// HTTPSigVerifyConfig holds options and policies for verifying RFC 9421 signatures.
	HTTPSigVerifyConfig = httpsig.VerifyConfig

	// HTTPSigParams contains parsed metadata parameters of an HTTP Message Signature.
	HTTPSigParams = httpsig.SignatureParams
)

// Standard RFC 9421 Algorithm constants.
const (
	AlgRSAPSSSHA512      = httpsig.AlgRSAPSSSHA512
	AlgRSAPKCS1v15SHA256 = httpsig.AlgRSAPKCS1v15SHA256
	AlgHMACSHA256        = httpsig.AlgHMACSHA256
	AlgECDSAP256SHA256   = httpsig.AlgECDSAP256SHA256
	AlgECDSAP384SHA384   = httpsig.AlgECDSAP384SHA384
	AlgEd25519           = httpsig.AlgEd25519
)

// SignHTTPSignature applies an RFC 9421 HTTP Message Signature to a standard [*http.Request].
func SignHTTPSignature(req *http.Request, cfg HTTPSigSignConfig) error {
	return httpsig.SignRequest(req, cfg)
}

// VerifyHTTPSignature verifies an RFC 9421 HTTP Message Signature on a standard [*http.Request].
func VerifyHTTPSignature(req *http.Request, cfg HTTPSigVerifyConfig) (*HTTPSigParams, error) {
	return httpsig.VerifyRequest(req, cfg)
}

// ComputeContentDigest calculates the RFC 9530 Content-Digest header value for body bytes.
func ComputeContentDigest(body []byte, algs ...string) string {
	return httpsig.ComputeContentDigest(body, algs...)
}

// VerifyContentDigest checks whether body matches the declared Content-Digest header value (RFC 9530).
func VerifyContentDigest(body []byte, headerVal string) error {
	return httpsig.VerifyContentDigest(body, headerVal)
}

// NewHTTPSigner creates an RFC 9421 [HTTPSigSigner] automatically from a private key.
func NewHTTPSigner(keyID string, key any, alg ...HTTPSigAlgorithm) (HTTPSigSigner, error) {
	return httpsig.NewSignerFromKey(keyID, key, alg...)
}

// NewHTTPVerifier creates an RFC 9421 [HTTPSigVerifier] automatically from a public key.
func NewHTTPVerifier(keyID string, key any, alg ...HTTPSigAlgorithm) (HTTPSigVerifier, error) {
	return httpsig.NewVerifierFromKey(keyID, key, alg...)
}
