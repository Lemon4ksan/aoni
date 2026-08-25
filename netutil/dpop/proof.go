// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dpop

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ComputeAccessTokenHash calculates the RFC 9449 §4.2 Access Token Hash ("ath") claim.
// It is the Base64URL-encoded (without padding) SHA-256 hash of the ASCII encoding of accessToken.
func ComputeAccessTokenHash(accessToken string) string {
	h := sha256.Sum256([]byte(accessToken))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// NormalizeTargetURI normalizes an HTTP request target URI for the "htu" claim (RFC 9449 §4.2).
// Strips query string and fragment, normalizes scheme and host to lowercase.
func NormalizeTargetURI(rawURI string) (string, error) {
	if rawURI == "" {
		return "", fmt.Errorf("%w: empty URI", ErrInvalidClaims)
	}

	u, err := url.Parse(rawURI)
	if err != nil {
		return "", fmt.Errorf("%w: malformed URI %q: %w", ErrInvalidClaims, rawURI, err)
	}

	// Normalization per RFC 9449 §4.2:
	// "The value MUST NOT contain a query or fragment component."
	u.RawQuery = ""
	u.Fragment = ""
	u.Scheme = strings.ToLower(u.Scheme)

	if u.Path == "" {
		u.Path = "/"
	}

	return u.String(), nil
}

// GenerateJTI generates a cryptographically random unique identifier string for the "jti" claim.
func GenerateJTI() string {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("jti-%d", time.Now().UnixNano())
	}

	return base64.RawURLEncoding.EncodeToString(b[:])
}

// CreateProof generates a signed DPoP Proof JWT according to RFC 9449 §4.2.
func CreateProof(privKey crypto.PrivateKey, method, targetURI string, opts ...ProofOptions) (string, error) {
	if privKey == nil {
		return "", fmt.Errorf("%w: private key cannot be nil", ErrUnsupportedKey)
	}

	var opt ProofOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	pubKey := extractPublicKey(privKey)
	if pubKey == nil {
		return "", fmt.Errorf("%w: unable to extract public key from %T", ErrUnsupportedKey, privKey)
	}

	jwk, defaultAlg, err := PublicKeyToJWK(pubKey)
	if err != nil {
		return "", err
	}

	// 1. Build JOSE Header
	header := ProofHeader{
		Typ: TypeDPoPJWT,
		Alg: defaultAlg,
		JWK: jwk,
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("dpop: failed to marshal header: %w", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	// 2. Build Payload Claims
	normURI, err := NormalizeTargetURI(targetURI)
	if err != nil {
		return "", err
	}

	jti := opt.CustomJTI
	if jti == "" {
		jti = GenerateJTI()
	}

	iat := opt.IAT
	if iat.IsZero() {
		iat = time.Now()
	}

	claims := ProofClaims{
		JTI:   jti,
		HTM:   strings.ToUpper(strings.TrimSpace(method)),
		HTU:   normURI,
		IAT:   iat.Unix(),
		Nonce: opt.Nonce,
	}

	if opt.AccessToken != "" {
		claims.ATH = ComputeAccessTokenHash(opt.AccessToken)
	}

	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("dpop: failed to marshal claims: %w", err)
	}

	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	// 3. Construct JWS Signing Input
	signingInput := headerB64 + "." + claimsB64

	// 4. Compute Signature
	sigBytes, err := signJWS(privKey, defaultAlg, []byte(signingInput))
	if err != nil {
		return "", err
	}

	sigB64 := base64.RawURLEncoding.EncodeToString(sigBytes)

	return signingInput + "." + sigB64, nil
}

// CreateProofForRequest creates a signed DPoP Proof JWT directly from an [*http.Request].
func CreateProofForRequest(req *http.Request, privKey crypto.PrivateKey, opts ...ProofOptions) (string, error) {
	if req == nil {
		return "", errors.New("dpop: nil request")
	}

	targetURL := ""
	if req.URL != nil {
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
	}

	return CreateProof(privKey, req.Method, targetURL, opts...)
}

func extractPublicKey(priv crypto.PrivateKey) crypto.PublicKey {
	switch k := priv.(type) {
	case *ecdsa.PrivateKey:
		return &k.PublicKey
	case ed25519.PrivateKey:
		return k.Public()
	case *rsa.PrivateKey:
		return &k.PublicKey
	case interface{ Public() crypto.PublicKey }:
		return k.Public()
	default:
		return nil
	}
}

func signJWS(priv crypto.PrivateKey, alg string, signingInput []byte) ([]byte, error) {
	switch k := priv.(type) {
	case *ecdsa.PrivateKey:
		switch alg {
		case AlgES256:
			h := sha256.Sum256(signingInput)

			r, s, err := ecdsa.Sign(rand.Reader, k, h[:])
			if err != nil {
				return nil, fmt.Errorf("dpop: ecdsa sign failed: %w", err)
			}

			sig := make([]byte, 64)
			r.FillBytes(sig[:32])
			s.FillBytes(sig[32:])

			return sig, nil

		case AlgES384:
			h := sha512.Sum384(signingInput)

			r, s, err := ecdsa.Sign(rand.Reader, k, h[:])
			if err != nil {
				return nil, fmt.Errorf("dpop: ecdsa sign failed: %w", err)
			}

			sig := make([]byte, 96)
			r.FillBytes(sig[:48])
			s.FillBytes(sig[48:])

			return sig, nil

		default:
			return nil, fmt.Errorf("%w: unsupported ECDSA alg %s", ErrUnsupportedKey, alg)
		}

	case ed25519.PrivateKey:
		return ed25519.Sign(k, signingInput), nil

	case *rsa.PrivateKey:
		switch alg {
		case AlgRS256:
			h := sha256.Sum256(signingInput)
			return rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, h[:])

		case AlgPS256:
			h := sha256.Sum256(signingInput)
			opts := &rsa.PSSOptions{
				SaltLength: rsa.PSSSaltLengthEqualsHash,
				Hash:       crypto.SHA256,
			}

			return rsa.SignPSS(rand.Reader, k, crypto.SHA256, h[:], opts)

		default:
			return nil, fmt.Errorf("%w: unsupported RSA alg %s", ErrUnsupportedKey, alg)
		}

	default:
		return nil, fmt.Errorf("%w: unsupported private key type %T", ErrUnsupportedKey, priv)
	}
}
