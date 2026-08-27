// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"net/http"
	"net/url"

	"github.com/lemon4ksan/aoni/internal/core"
	"github.com/lemon4ksan/aoni/netutil/httpsig"
)

// WithHTTPSignature signs request components according to RFC 9421 (HTTP Message Signatures).
//
// Automatically injects the "Signature-Input" and "Signature" HTTP headers.
//
// # RFC Compliance
//
// Conforms to RFC 9421 (HTTP Message Signatures).
func WithHTTPSignature(cfg httpsig.SignConfig) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			if cfg.Signer == nil {
				return
			}

			// Construct URL object from Request
			reqURLStr := req.URL()

			u, err := url.Parse(reqURLStr)
			if err != nil {
				return
			}

			// Construct headers view
			header := make(http.Header)
			for k, v := range req.Headers() {
				header.Add(string(k), string(v))
			}

			ctx := &httpsig.RequestContext{
				Method:     req.Method(),
				URL:        u,
				Header:     header,
				IsResponse: false,
			}

			sigInputMember, sigMember, err := httpsig.Sign(ctx, cfg)
			if err != nil {
				return
			}

			existingInput := req.Header(httpsig.HeaderSignatureInput)
			if existingInput == "" {
				req.SetHeader(httpsig.HeaderSignatureInput, sigInputMember)
			} else {
				req.SetHeader(httpsig.HeaderSignatureInput, existingInput+", "+sigInputMember)
			}

			existingSig := req.Header(httpsig.HeaderSignature)
			if existingSig == "" {
				req.SetHeader(httpsig.HeaderSignature, sigMember)
			} else {
				req.SetHeader(httpsig.HeaderSignature, existingSig+", "+sigMember)
			}
		},
	}
}

// WithHTTPSigner applies an RFC 9421 HTTP Message Signature using a concrete [httpsig.Signer].
func WithHTTPSigner(signer httpsig.Signer, components ...string) RequestModifier {
	cfg := httpsig.SignConfig{
		Signer:     signer,
		Components: components,
	}

	return WithHTTPSignature(cfg)
}

// WithHTTPSignatureKey signs the request using an asymmetric private key (Ed25519, ECDSA, RSA) or HMAC secret.
//
// # Example
//
//	resp, err := client.Post(ctx, "/signed-webhook",
//	    mod.WithHTTPSignatureKey("key-1", ed25519PrivateKey, "@method", "@target-uri", "content-digest"),
//	    mod.WithContentDigest("sha-256"),
//	    mod.WithJSONBody(payload),
//	)
func WithHTTPSignatureKey(keyID string, privKey any, components ...string) RequestModifier {
	signer, err := httpsig.NewSignerFromKey(keyID, privKey)
	if err != nil {
		return RequestModifier{}
	}

	return WithHTTPSigner(signer, components...)
}

// WithContentDigest calculates and injects an RFC 9530 "Content-Digest" header over the request body bytes.
//
// Supported algorithms: "sha-256", "sha-512".
//
// # RFC Compliance
//
// Conforms to RFC 9530 (Digest Fields).
func WithContentDigest(algs ...string) RequestModifier {
	return RequestModifier{
		Kind: core.ModCustom,
		Fn: func(req Request) {
			body := req.BodyBytes()
			digestVal := httpsig.ComputeContentDigest(body, algs...)
			req.SetHeader(httpsig.HeaderContentDigest, digestVal)
		},
	}
}
