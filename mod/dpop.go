// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"crypto"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/netutil/dpop"
)

// WithDPoP generates and injects an RFC 9449 Demonstrating Proof-of-Possession (DPoP) Proof JWT
// into the "DPoP" request header.
//
// Signs the HTTP method and full URI using the provided asymmetric private key.
//
// # RFC Compliance
//
// Conforms to RFC 9449 (OAuth 2.0 Demonstrating Proof-of-Possession at the Application Layer).
func WithDPoP(privKey crypto.PrivateKey, opts ...dpop.ProofOptions) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			proof, err := dpop.CreateProof(privKey, req.Method(), req.URL(), opts...)
			if err != nil {
				return
			}

			req.SetHeader(dpop.HeaderDPoP, proof)
		},
	}
}

// WithDPoPToken attaches a DPoP-bound OAuth 2.0 access token ("Authorization: DPoP <token>")
// and generates a matching DPoP Proof JWT with the access token hash ("ath" claim) into "DPoP" (RFC 9449 §7.1).
//
// # Example
//
//	resp, err := client.Get(ctx, "/protected/resource",
//	    mod.WithDPoPToken(accessToken, privateKey),
//	)
//
// # RFC Compliance
//
// Conforms to RFC 9449 Section 7.1.
func WithDPoPToken(accessToken string, privKey crypto.PrivateKey, opts ...dpop.ProofOptions) RequestModifier {
	var opt dpop.ProofOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	opt.AccessToken = accessToken

	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			proof, err := dpop.CreateProof(privKey, req.Method(), req.URL(), opt)
			if err != nil {
				return
			}

			req.SetHeader(dpop.HeaderAuthorization, dpop.SchemeDPoP+" "+accessToken)
			req.SetHeader(dpop.HeaderDPoP, proof)
		},
	}
}

// WithDPoPProof attaches a pre-computed RFC 9449 DPoP Proof JWT string directly into the "DPoP" header.
func WithDPoPProof(proofJWT string) RequestModifier {
	return WithHeader(dpop.HeaderDPoP, proofJWT)
}
