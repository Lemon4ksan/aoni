// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dpop

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/big"
)

// PublicKeyToJWK converts a public key into a public [JWK] and returns its default JWS algorithm (RFC 7517).
func PublicKeyToJWK(pub crypto.PublicKey) (*JWK, string, error) {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		switch k.Curve {
		case elliptic.P256():
			byteLen := 32
			xBuf := make([]byte, byteLen)
			yBuf := make([]byte, byteLen)

			k.X.FillBytes(xBuf)
			k.Y.FillBytes(yBuf)

			return &JWK{
				Kty: "EC",
				Crv: "P-256",
				X:   base64.RawURLEncoding.EncodeToString(xBuf),
				Y:   base64.RawURLEncoding.EncodeToString(yBuf),
			}, AlgES256, nil

		case elliptic.P384():
			byteLen := 48
			xBuf := make([]byte, byteLen)
			yBuf := make([]byte, byteLen)

			k.X.FillBytes(xBuf)
			k.Y.FillBytes(yBuf)

			return &JWK{
				Kty: "EC",
				Crv: "P-384",
				X:   base64.RawURLEncoding.EncodeToString(xBuf),
				Y:   base64.RawURLEncoding.EncodeToString(yBuf),
			}, AlgES384, nil

		default:
			return nil, "", fmt.Errorf("%w: unsupported ECDSA curve %s", ErrUnsupportedKey, k.Curve.Params().Name)
		}

	case ed25519.PublicKey:
		if len(k) != ed25519.PublicKeySize {
			return nil, "", fmt.Errorf("%w: invalid ed25519 public key length", ErrUnsupportedKey)
		}

		return &JWK{
			Kty: "OKP",
			Crv: "Ed25519",
			X:   base64.RawURLEncoding.EncodeToString(k),
		}, AlgEdDSA, nil

	case *rsa.PublicKey:
		nBytes := k.N.Bytes()
		eBytes := big.NewInt(int64(k.E)).Bytes()

		return &JWK{
			Kty: "RSA",
			N:   base64.RawURLEncoding.EncodeToString(nBytes),
			E:   base64.RawURLEncoding.EncodeToString(eBytes),
		}, AlgRS256, nil

	default:
		return nil, "", fmt.Errorf("%w: unsupported public key type %T", ErrUnsupportedKey, pub)
	}
}

// JWKToPublicKey converts a public [JWK] into a standard [crypto.PublicKey].
func JWKToPublicKey(jwk *JWK) (crypto.PublicKey, error) {
	if jwk == nil {
		return nil, ErrInvalidHeader
	}

	switch jwk.Kty {
	case "EC":
		var (
			curve       elliptic.Curve
			expectedLen int
		)

		switch jwk.Crv {
		case "P-256":
			curve = elliptic.P256()
			expectedLen = 32
		case "P-384":
			curve = elliptic.P384()
			expectedLen = 48
		default:
			return nil, fmt.Errorf("%w: unsupported EC curve %s", ErrUnsupportedKey, jwk.Crv)
		}

		xBytes, err := base64.RawURLEncoding.DecodeString(jwk.X)
		if err != nil || len(xBytes) != expectedLen {
			return nil, fmt.Errorf("%w: invalid x coordinate in EC JWK", ErrInvalidHeader)
		}

		yBytes, err := base64.RawURLEncoding.DecodeString(jwk.Y)
		if err != nil || len(yBytes) != expectedLen {
			return nil, fmt.Errorf("%w: invalid y coordinate in EC JWK", ErrInvalidHeader)
		}

		return &ecdsa.PublicKey{
			Curve: curve,
			X:     new(big.Int).SetBytes(xBytes),
			Y:     new(big.Int).SetBytes(yBytes),
		}, nil

	case "OKP":
		if jwk.Crv != "Ed25519" {
			return nil, fmt.Errorf("%w: unsupported OKP curve %s", ErrUnsupportedKey, jwk.Crv)
		}

		pubBytes, err := base64.RawURLEncoding.DecodeString(jwk.X)
		if err != nil || len(pubBytes) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%w: invalid ed25519 public key bytes", ErrInvalidHeader)
		}

		return ed25519.PublicKey(pubBytes), nil

	case "RSA":
		nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
		if err != nil || len(nBytes) == 0 {
			return nil, fmt.Errorf("%w: invalid modulus in RSA JWK", ErrInvalidHeader)
		}

		eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
		if err != nil || len(eBytes) == 0 {
			return nil, fmt.Errorf("%w: invalid exponent in RSA JWK", ErrInvalidHeader)
		}

		eInt := new(big.Int).SetBytes(eBytes).Int64()

		return &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: int(eInt),
		}, nil

	default:
		return nil, fmt.Errorf("%w: unsupported kty %s", ErrUnsupportedKey, jwk.Kty)
	}
}

// ComputeThumbprint calculates the RFC 7638 JWK Thumbprint (SHA-256 hash in Base64URL encoding).
// The thumbprint can be used as the "jkt" confirmation claim ("cnf: { jkt: ... }") in OAuth 2.0.
func ComputeThumbprint(jwk *JWK) (string, error) {
	if jwk == nil {
		return "", ErrInvalidHeader
	}

	var canonicalJSON string

	switch jwk.Kty {
	case "EC":
		// Lexicographical order: crv, kty, x, y (RFC 7638 §3.2)
		canonicalJSON = `{"crv":"` + jwk.Crv + `","kty":"EC","x":"` + jwk.X + `","y":"` + jwk.Y + `"}`

	case "OKP":
		// Lexicographical order: crv, kty, x (RFC 7638 §3.2)
		canonicalJSON = `{"crv":"` + jwk.Crv + `","kty":"OKP","x":"` + jwk.X + `"}`

	case "RSA":
		// Lexicographical order: e, kty, n (RFC 7638 §3.2)
		canonicalJSON = `{"e":"` + jwk.E + `","kty":"RSA","n":"` + jwk.N + `"}`

	default:
		return "", fmt.Errorf("%w: unsupported kty %s for thumbprint", ErrUnsupportedKey, jwk.Kty)
	}

	h := sha256.Sum256([]byte(canonicalJSON))

	return base64.RawURLEncoding.EncodeToString(h[:]), nil
}
