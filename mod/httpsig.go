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

// WithHTTPSignature constructs an [RequestModifier] signing request components per RFC 9421.
// Injects the "Signature-Input" and "Signature" HTTP headers.
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

// WithHTTPSigner constructs an [RequestModifier] applying an RFC 9421 HTTP Message Signature
// using the provided [httpsig.Signer].
func WithHTTPSigner(signer httpsig.Signer, components ...string) RequestModifier {
	cfg := httpsig.SignConfig{
		Signer:     signer,
		Components: components,
	}

	return WithHTTPSignature(cfg)
}

// WithHTTPSignatureKey constructs an [RequestModifier] signing the request using an asymmetric
// private key (Ed25519, ECDSA, RSA) or HMAC shared secret with automatic algorithm detection.
func WithHTTPSignatureKey(keyID string, privKey any, components ...string) RequestModifier {
	signer, err := httpsig.NewSignerFromKey(keyID, privKey)
	if err != nil {
		return RequestModifier{}
	}

	return WithHTTPSigner(signer, components...)
}

// WithContentDigest constructs an [RequestModifier] calculating and injecting an RFC 9530
// "Content-Digest" header (e.g. "sha-256=:...:") over the request body payload.
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
