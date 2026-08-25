// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"crypto"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/netutil/dpop"
)

// WithDPoP constructs an [aoni.RequestModifier] injecting an RFC 9449 DPoP Proof JWT
// into the "DPoP" request header.
func WithDPoP(privKey crypto.PrivateKey, opts ...dpop.ProofOptions) aoni.RequestModifier {
	return aoni.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req aoni.Request) {
			proof, err := dpop.CreateProof(privKey, req.Method(), req.URL(), opts...)
			if err != nil {
				return
			}

			req.SetHeader(dpop.HeaderDPoP, proof)
		},
	}
}

// WithDPoPToken constructs an [aoni.RequestModifier] attaching a DPoP-bound OAuth 2.0 access token
// to the "Authorization" header ("DPoP <accessToken>") and calculating the DPoP Proof JWT with
// the access token hash ("ath" claim) into the "DPoP" header per RFC 9449 §7.1.
func WithDPoPToken(accessToken string, privKey crypto.PrivateKey, opts ...dpop.ProofOptions) aoni.RequestModifier {
	var opt dpop.ProofOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	opt.AccessToken = accessToken

	return aoni.RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req aoni.Request) {
			proof, err := dpop.CreateProof(privKey, req.Method(), req.URL(), opt)
			if err != nil {
				return
			}

			req.SetHeader(dpop.HeaderAuthorization, dpop.SchemeDPoP+" "+accessToken)
			req.SetHeader(dpop.HeaderDPoP, proof)
		},
	}
}

// WithDPoPProof constructs an [aoni.RequestModifier] attaching a pre-computed DPoP Proof JWT string.
func WithDPoPProof(proofJWT string) aoni.RequestModifier {
	return WithHeader(dpop.HeaderDPoP, proofJWT)
}
