// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package httpsig

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Sign signs an abstract HTTP [RequestContext] according to RFC 9421.
// Returns the serialized Signature-Input and Signature header member strings.
func Sign(ctx *RequestContext, cfg SignConfig) (sigInputMember, sigMember string, err error) {
	if ctx == nil {
		return "", "", errors.New("httpsig: nil request context")
	}

	if cfg.Signer == nil {
		return "", "", errors.New("httpsig: signer is required")
	}

	label := cfg.Label
	if label == "" {
		label = "sig1"
	}

	components := cfg.Components
	if len(components) == 0 {
		if ctx.IsResponse {
			components = []string{CompStatus}
		} else {
			components = []string{CompMethod, CompAuthority, CompPath}
		}
	}

	created := cfg.Created
	if created.IsZero() {
		created = time.Now()
	}

	params := &SignatureParams{
		Label:      label,
		Components: components,
		Created:    created.Unix(),
		Alg:        cfg.Signer.Algorithm(),
		KeyID:      cfg.Signer.KeyID(),
		Nonce:      cfg.Nonce,
		Tag:        cfg.Tag,
	}

	if !cfg.Expires.IsZero() {
		params.Expires = cfg.Expires.Unix()
	}

	baseBytes, err := BuildSignatureBase(ctx, params)
	if err != nil {
		return "", "", fmt.Errorf("httpsig: failed to build signature base: %w", err)
	}

	sigBytes, err := cfg.Signer.Sign(baseBytes)
	if err != nil {
		return "", "", fmt.Errorf("httpsig: cryptographic signing failed: %w", err)
	}

	serializedParams, err := SerializeSignatureParams(params)
	if err != nil {
		return "", "", fmt.Errorf("httpsig: failed to serialize signature params: %w", err)
	}

	sigB64 := base64.StdEncoding.EncodeToString(sigBytes)

	sigInputMember = label + "=" + serializedParams
	sigMember = label + "=:" + sigB64 + ":"

	return sigInputMember, sigMember, nil
}

// SignRequest applies an RFC 9421 HTTP Message Signature directly to a standard [*http.Request].
func SignRequest(req *http.Request, cfg SignConfig) error {
	if req == nil {
		return errors.New("httpsig: nil request")
	}

	ctx := &RequestContext{
		Method:     req.Method,
		URL:        req.URL,
		Header:     req.Header,
		IsResponse: false,
	}

	inputMember, sigMember, err := Sign(ctx, cfg)
	if err != nil {
		return err
	}

	appendHeader(req.Header, HeaderSignatureInput, inputMember)
	appendHeader(req.Header, HeaderSignature, sigMember)

	return nil
}

// SignResponse applies an RFC 9421 HTTP Message Signature to a standard [*http.Response],
// optionally incorporating covered components from the triggering request (RFC 9421 §2.4).
func SignResponse(resp *http.Response, origReq *http.Request, cfg SignConfig) error {
	if resp == nil {
		return errors.New("httpsig: nil response")
	}

	ctx := &RequestContext{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Trailers:   resp.Trailer,
		IsResponse: true,
	}

	if origReq != nil {
		ctx.ReqContext = &RequestContext{
			Method:     origReq.Method,
			URL:        origReq.URL,
			Header:     origReq.Header,
			IsResponse: false,
		}
	}

	inputMember, sigMember, err := Sign(ctx, cfg)
	if err != nil {
		return err
	}

	appendHeader(resp.Header, HeaderSignatureInput, inputMember)
	appendHeader(resp.Header, HeaderSignature, sigMember)

	return nil
}

func appendHeader(header http.Header, key, member string) {
	existing := header.Get(key)
	if existing == "" {
		header.Set(key, member)
	} else {
		header.Set(key, existing+", "+member)
	}
}
