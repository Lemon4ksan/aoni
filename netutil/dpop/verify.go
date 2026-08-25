// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dpop

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// VerifyProof verifies an RFC 9449 DPoP Proof JWT string against validation policies.
// Returns the verified claims and the public JWK on success.
func VerifyProof(proofJWT string, cfg VerifierConfig) (*ProofClaims, *JWK, error) {
	proofJWT = strings.TrimSpace(proofJWT)
	if proofJWT == "" {
		return nil, nil, fmt.Errorf("%w: empty DPoP proof", ErrInvalidDPoPProof)
	}

	parts := strings.Split(proofJWT, ".")
	if len(parts) != 3 {
		return nil, nil, fmt.Errorf("%w: expected 3 parts separated by '.'", ErrInvalidDPoPProof)
	}

	headerB64, claimsB64, sigB64 := parts[0], parts[1], parts[2]

	// 1. Decode & Validate Header
	headerBytes, err := base64.RawURLEncoding.DecodeString(headerB64)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: malformed header base64: %w", ErrInvalidHeader, err)
	}

	var header ProofHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, nil, fmt.Errorf("%w: malformed header json: %w", ErrInvalidHeader, err)
	}

	if !strings.EqualFold(header.Typ, TypeDPoPJWT) {
		return nil, nil, fmt.Errorf("%w: header typ must be %q, got %q", ErrInvalidHeader, TypeDPoPJWT, header.Typ)
	}

	if header.JWK == nil {
		return nil, nil, fmt.Errorf("%w: missing jwk parameter in header", ErrInvalidHeader)
	}

	// Reject symmetric algorithms or none per RFC 9449 §4.2
	if strings.EqualFold(header.Alg, "none") || strings.HasPrefix(strings.ToUpper(header.Alg), "HS") {
		return nil, nil, fmt.Errorf("%w: symmetric and none algorithms are strictly prohibited", ErrUnsupportedKey)
	}

	pubKey, err := JWKToPublicKey(header.JWK)
	if err != nil {
		return nil, nil, err
	}

	// 2. Decode & Cryptographically Verify Signature
	sigBytes, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: malformed signature base64: %w", ErrInvalidSignature, err)
	}

	signingInput := headerB64 + "." + claimsB64
	if err := verifyJWSSignature(pubKey, header.Alg, []byte(signingInput), sigBytes); err != nil {
		return nil, nil, err
	}

	// 3. Decode & Validate Claims
	claimsBytes, err := base64.RawURLEncoding.DecodeString(claimsB64)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: malformed claims base64: %w", ErrInvalidClaims, err)
	}

	var claims ProofClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, nil, fmt.Errorf("%w: malformed claims json: %w", ErrInvalidClaims, err)
	}

	if claims.JTI == "" {
		return nil, nil, fmt.Errorf("%w: missing jti claim", ErrInvalidClaims)
	}

	// 4. Validate Method (htm)
	if cfg.ExpectedMethod != "" {
		if !strings.EqualFold(claims.HTM, cfg.ExpectedMethod) {
			return nil, nil, fmt.Errorf(
				"%w: expected method %q, got %q",
				ErrMethodMismatch,
				cfg.ExpectedMethod,
				claims.HTM,
			)
		}
	}

	// 5. Validate Target URI (htu)
	if cfg.ExpectedURI != "" {
		expectedNorm, err := NormalizeTargetURI(cfg.ExpectedURI)
		if err != nil {
			return nil, nil, err
		}

		if claims.HTU != expectedNorm {
			return nil, nil, fmt.Errorf("%w: expected URI %q, got %q", ErrURIMismatch, expectedNorm, claims.HTU)
		}
	}

	// 6. Validate Timestamps (iat)
	now := time.Now()

	skew := cfg.AllowedClockSkew
	if skew <= 0 {
		skew = time.Minute
	}

	maxAge := cfg.MaxAge
	if maxAge <= 0 {
		maxAge = 5 * time.Minute
	}

	iatTime := time.Unix(claims.IAT, 0)
	if iatTime.After(now.Add(skew)) {
		return nil, nil, ErrProofInFuture
	}

	if now.Sub(iatTime) > maxAge+skew {
		return nil, nil, ErrProofExpired
	}

	// 7. Validate Access Token Hash (ath)
	if cfg.AccessToken != "" {
		expectedATH := ComputeAccessTokenHash(cfg.AccessToken)
		if subtle.ConstantTimeCompare([]byte(claims.ATH), []byte(expectedATH)) != 1 {
			return nil, nil, fmt.Errorf("%w: ath mismatch", ErrAccessTokenMismatch)
		}
	}

	// 8. Validate Server Nonce (nonce)
	if cfg.ExpectedNonce != "" {
		if subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(cfg.ExpectedNonce)) != 1 {
			return nil, nil, fmt.Errorf("%w: nonce mismatch", ErrNonceMismatch)
		}
	}

	return &claims, header.JWK, nil
}

// VerifyProofForRequest extracts and validates the DPoP Proof from an incoming [*http.Request].
func VerifyProofForRequest(req *http.Request, cfg VerifierConfig) (*ProofClaims, *JWK, error) {
	if req == nil {
		return nil, nil, errors.New("dpop: nil request")
	}

	proofHeader := req.Header.Get(HeaderDPoP)
	if proofHeader == "" {
		return nil, nil, fmt.Errorf("%w: DPoP header missing from request", ErrInvalidDPoPProof)
	}

	if cfg.ExpectedMethod == "" {
		cfg.ExpectedMethod = req.Method
	}

	if cfg.ExpectedURI == "" && req.URL != nil {
		targetURL := ""
		switch {
		case req.URL.IsAbs():
			targetURL = req.URL.String()
		case req.Host != "":
			scheme := "https"
			if req.TLS == nil {
				scheme = "http"
			}

			targetURL = scheme + "://" + req.Host + req.URL.RequestURI()

		default:
			targetURL = req.URL.String()
		}

		cfg.ExpectedURI = targetURL
	}

	// Auto-extract DPoP access token from Authorization header if not explicitly configured
	if cfg.AccessToken == "" {
		authVal := req.Header.Get(HeaderAuthorization)
		if strings.HasPrefix(authVal, SchemeDPoP+" ") {
			cfg.AccessToken = strings.TrimSpace(authVal[len(SchemeDPoP)+1:])
		}
	}

	return VerifyProof(proofHeader, cfg)
}

func verifyJWSSignature(pub crypto.PublicKey, alg string, signingInput, sig []byte) error {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		switch alg {
		case AlgES256:
			if len(sig) != 64 {
				return ErrInvalidSignature
			}

			h := sha256.Sum256(signingInput)
			r := new(big.Int).SetBytes(sig[:32])
			s := new(big.Int).SetBytes(sig[32:])

			if !ecdsa.Verify(k, h[:], r, s) {
				return ErrInvalidSignature
			}

			return nil

		case AlgES384:
			if len(sig) != 96 {
				return ErrInvalidSignature
			}

			h := sha512.Sum384(signingInput)
			r := new(big.Int).SetBytes(sig[:48])
			s := new(big.Int).SetBytes(sig[48:])

			if !ecdsa.Verify(k, h[:], r, s) {
				return ErrInvalidSignature
			}

			return nil

		default:
			return fmt.Errorf("%w: unsupported ECDSA alg %s", ErrUnsupportedKey, alg)
		}

	case ed25519.PublicKey:
		if len(sig) != ed25519.SignatureSize {
			return ErrInvalidSignature
		}

		if !ed25519.Verify(k, signingInput, sig) {
			return ErrInvalidSignature
		}

		return nil

	case *rsa.PublicKey:
		switch alg {
		case AlgRS256:
			h := sha256.Sum256(signingInput)
			if err := rsa.VerifyPKCS1v15(k, crypto.SHA256, h[:], sig); err != nil {
				return ErrInvalidSignature
			}

			return nil

		case AlgPS256:
			h := sha256.Sum256(signingInput)
			opts := &rsa.PSSOptions{
				SaltLength: rsa.PSSSaltLengthEqualsHash,
				Hash:       crypto.SHA256,
			}

			if err := rsa.VerifyPSS(k, crypto.SHA256, h[:], sig, opts); err != nil {
				return ErrInvalidSignature
			}

			return nil

		default:
			return fmt.Errorf("%w: unsupported RSA alg %s", ErrUnsupportedKey, alg)
		}

	default:
		return fmt.Errorf("%w: unsupported public key type %T", ErrUnsupportedKey, pub)
	}
}
