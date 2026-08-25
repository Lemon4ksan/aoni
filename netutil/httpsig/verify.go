// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package httpsig

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Verify verifies an abstract HTTP [RequestContext] against an RFC 9421 signature.
func Verify(ctx *RequestContext, cfg VerifyConfig) (*SignatureParams, error) {
	if ctx == nil {
		return nil, errors.New("httpsig: nil request context")
	}

	if cfg.Verifier == nil {
		return nil, errors.New("httpsig: verifier is required")
	}

	sigInputHeader := ctx.Header.Get(HeaderSignatureInput)
	sigHeader := ctx.Header.Get(HeaderSignature)

	if sigInputHeader == "" || sigHeader == "" {
		return nil, ErrMissingSignature
	}

	// Parse Signature-Input dictionary members
	inputEntries, err := parseDictionaryMembers(sigInputHeader)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to parse %s: %w", ErrInvalidStructuredField, HeaderSignatureInput, err)
	}

	// Parse Signature dictionary members
	sigEntries, err := parseDictionaryMembers(sigHeader)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to parse %s: %w", ErrInvalidStructuredField, HeaderSignature, err)
	}

	targetLabel := cfg.Label
	if targetLabel == "" {
		// Default to first signature label in Signature-Input
		if len(inputEntries) == 0 {
			return nil, ErrMissingSignature
		}

		targetLabel = inputEntries[0].Label
	}

	// Find Signature-Input for targetLabel
	var rawParams string
	for _, entry := range inputEntries {
		if entry.Label == targetLabel {
			rawParams = entry.Value
			break
		}
	}

	if rawParams == "" {
		return nil, fmt.Errorf("%w: label %q not found in %s", ErrLabelMismatch, targetLabel, HeaderSignatureInput)
	}

	// Find Signature for targetLabel
	var rawSig string
	for _, entry := range sigEntries {
		if entry.Label == targetLabel {
			rawSig = entry.Value
			break
		}
	}

	if rawSig == "" {
		return nil, fmt.Errorf("%w: label %q not found in %s", ErrLabelMismatch, targetLabel, HeaderSignature)
	}

	// Parse signature parameters
	params, err := ParseSignatureParams(rawParams)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to parse signature parameters: %w", ErrInvalidStructuredField, err)
	}

	params.Label = targetLabel

	// Parse binary signature bytes
	sigBytes, err := decodeByteSequence(rawSig)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid signature byte sequence %q: %w", ErrInvalidStructuredField, rawSig, err)
	}

	// Validate algorithm match if present
	if params.Alg != "" && params.Alg != cfg.Verifier.Algorithm() {
		return nil, fmt.Errorf(
			"%w: signature alg %s != verifier alg %s",
			ErrUnsupportedAlgorithm,
			params.Alg,
			cfg.Verifier.Algorithm(),
		)
	}

	// Validate key ID match if present
	if params.KeyID != "" && cfg.Verifier.KeyID() != "" && params.KeyID != cfg.Verifier.KeyID() {
		return nil, fmt.Errorf(
			"%w: signature keyid %s != verifier keyid %s",
			ErrKeyMismatch,
			params.KeyID,
			cfg.Verifier.KeyID(),
		)
	}

	// Validate required application tag
	if cfg.RequiredTag != "" && params.Tag != cfg.RequiredTag {
		return nil, fmt.Errorf(
			"%w: signature tag %q does not match required %q",
			ErrInvalidSignature,
			params.Tag,
			cfg.RequiredTag,
		)
	}

	// Validate time constraints
	now := time.Now()

	skew := cfg.AllowedClockSkew
	if skew <= 0 {
		skew = time.Minute
	}

	if params.Expires > 0 {
		expTime := time.Unix(params.Expires, 0)
		if now.After(expTime.Add(skew)) {
			return nil, ErrSignatureExpired
		}
	}

	if params.Created > 0 {
		createdTime := time.Unix(params.Created, 0)
		if createdTime.After(now.Add(skew)) {
			return nil, ErrSignatureInFuture
		}

		if cfg.MaxAge > 0 && now.Sub(createdTime) > cfg.MaxAge+skew {
			return nil, ErrSignatureTooOld
		}
	}

	// Validate required covered components
	if len(cfg.RequiredComponents) > 0 {
		coveredSet := make(map[string]struct{}, len(params.Components))
		for _, c := range params.Components {
			parsed, err := ParseComponentIdentifier(c)
			if err == nil {
				coveredSet[parsed.Serialized] = struct{}{}
				coveredSet[parsed.Name] = struct{}{}
			}
		}

		for _, reqComp := range cfg.RequiredComponents {
			parsedReq, err := ParseComponentIdentifier(reqComp)
			if err != nil {
				return nil, err
			}

			_, hasSerialized := coveredSet[parsedReq.Serialized]

			_, hasName := coveredSet[parsedReq.Name]
			if !hasSerialized && !hasName {
				return nil, fmt.Errorf(
					"%w: required component %q not covered by signature",
					ErrMissingComponent,
					reqComp,
				)
			}
		}
	}

	// Reconstruct signature base
	baseBytes, err := BuildSignatureBase(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("httpsig: failed to rebuild signature base: %w", err)
	}

	// Perform cryptographic verification
	if err := cfg.Verifier.Verify(baseBytes, sigBytes); err != nil {
		return nil, err
	}

	return params, nil
}

// VerifyRequest verifies an RFC 9421 signature on a standard [*http.Request].
func VerifyRequest(req *http.Request, cfg VerifyConfig) (*SignatureParams, error) {
	if req == nil {
		return nil, errors.New("httpsig: nil request")
	}

	ctx := &RequestContext{
		Method:     req.Method,
		URL:        req.URL,
		Header:     req.Header,
		IsResponse: false,
	}

	return Verify(ctx, cfg)
}

// VerifyResponse verifies an RFC 9421 signature on a standard [*http.Response],
// supplying the original request for any request-derived components (req flag).
func VerifyResponse(resp *http.Response, origReq *http.Request, cfg VerifyConfig) (*SignatureParams, error) {
	if resp == nil {
		return nil, errors.New("httpsig: nil response")
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

	return Verify(ctx, cfg)
}

type dictEntry struct {
	Label string
	Value string
}

func parseDictionaryMembers(headerVal string) ([]dictEntry, error) {
	var entries []dictEntry

	items := splitHeaderComma(headerVal)

	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		eqIdx := strings.IndexByte(item, '=')
		if eqIdx == -1 {
			continue
		}

		label := strings.TrimSpace(item[:eqIdx])
		val := strings.TrimSpace(item[eqIdx+1:])

		entries = append(entries, dictEntry{Label: label, Value: val})
	}

	return entries, nil
}

func splitHeaderComma(val string) []string {
	var (
		parts   []string
		current strings.Builder
	)

	inQuote := false
	inParen := 0
	inColon := false

	for i := 0; i < len(val); i++ {
		ch := val[i]
		switch {
		case ch == '"' && !inColon:
			inQuote = !inQuote

			current.WriteByte(ch)
		case ch == '(' && !inQuote:
			inParen++

			current.WriteByte(ch)
		case ch == ')' && !inQuote && inParen > 0:
			inParen--

			current.WriteByte(ch)
		case ch == ':' && !inQuote:
			inColon = !inColon

			current.WriteByte(ch)
		case ch == ',' && !inQuote && inParen == 0 && !inColon:
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

func decodeByteSequence(val string) ([]byte, error) {
	val = strings.TrimSpace(val)
	if strings.HasPrefix(val, ":") && strings.HasSuffix(val, ":") && len(val) >= 2 {
		val = val[1 : len(val)-1]
	}

	return base64.StdEncoding.DecodeString(val)
}
