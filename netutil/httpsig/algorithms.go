// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package httpsig

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
)

// NewSignerFromKey automatically constructs an appropriate [Signer] based on private key type and optional algorithm.
func NewSignerFromKey(keyID string, key any, alg ...Algorithm) (Signer, error) {
	if key == nil {
		return nil, fmt.Errorf("%w: private key cannot be nil", ErrKeyMismatch)
	}

	var chosenAlg Algorithm
	if len(alg) > 0 && alg[0] != "" {
		chosenAlg = alg[0]
	}

	switch k := key.(type) {
	case ed25519.PrivateKey:
		return NewEd25519Signer(keyID, k)

	case *ecdsa.PrivateKey:
		if chosenAlg == "" {
			switch k.Curve {
			case elliptic.P256():
				chosenAlg = AlgECDSAP256SHA256
			case elliptic.P384():
				chosenAlg = AlgECDSAP384SHA384
			default:
				return nil, fmt.Errorf("%w: unsupported ECDSA curve %s", ErrUnsupportedAlgorithm, k.Curve.Params().Name)
			}
		}

		return NewECDSASigner(keyID, chosenAlg, k)

	case *rsa.PrivateKey:
		if chosenAlg == "" {
			chosenAlg = AlgRSAPSSSHA512
		}

		return NewRSASigner(keyID, chosenAlg, k)

	case []byte:
		return NewHMACSigner(keyID, k)

	case string:
		return NewHMACSigner(keyID, []byte(k))

	default:
		return nil, fmt.Errorf("%w: unsupported private key type %T", ErrKeyMismatch, key)
	}
}

// NewVerifierFromKey automatically constructs an appropriate [Verifier] based on public key type and optional algorithm.
func NewVerifierFromKey(keyID string, key any, alg ...Algorithm) (Verifier, error) {
	if key == nil {
		return nil, fmt.Errorf("%w: public key cannot be nil", ErrKeyMismatch)
	}

	var chosenAlg Algorithm
	if len(alg) > 0 && alg[0] != "" {
		chosenAlg = alg[0]
	}

	switch k := key.(type) {
	case ed25519.PublicKey:
		return NewEd25519Verifier(keyID, k)

	case *ecdsa.PublicKey:
		if chosenAlg == "" {
			switch k.Curve {
			case elliptic.P256():
				chosenAlg = AlgECDSAP256SHA256
			case elliptic.P384():
				chosenAlg = AlgECDSAP384SHA384
			default:
				return nil, fmt.Errorf("%w: unsupported ECDSA curve %s", ErrUnsupportedAlgorithm, k.Curve.Params().Name)
			}
		}

		return NewECDSAVerifier(keyID, chosenAlg, k)

	case *rsa.PublicKey:
		if chosenAlg == "" {
			chosenAlg = AlgRSAPSSSHA512
		}

		return NewRSAVerifier(keyID, chosenAlg, k)

	case []byte:
		return NewHMACVerifier(keyID, k)

	case string:
		return NewHMACVerifier(keyID, []byte(k))

	default:
		return nil, fmt.Errorf("%w: unsupported public key type %T", ErrKeyMismatch, key)
	}
}

// ============================================================================
// HMAC-SHA256 (RFC 9421 §3.3.3)
// ============================================================================

type hmacSignerVerifier struct {
	keyID  string
	secret []byte
}

// NewHMACSigner constructs an HMAC-SHA256 signer.
func NewHMACSigner(keyID string, secret []byte) (Signer, error) {
	if len(secret) == 0 {
		return nil, errors.New("httpsig: HMAC secret key cannot be empty")
	}

	return &hmacSignerVerifier{keyID: keyID, secret: secret}, nil
}

// NewHMACVerifier constructs an HMAC-SHA256 verifier.
func NewHMACVerifier(keyID string, secret []byte) (Verifier, error) {
	if len(secret) == 0 {
		return nil, errors.New("httpsig: HMAC secret key cannot be empty")
	}

	return &hmacSignerVerifier{keyID: keyID, secret: secret}, nil
}

func (h *hmacSignerVerifier) Algorithm() Algorithm { return AlgHMACSHA256 }
func (h *hmacSignerVerifier) KeyID() string        { return h.keyID }

func (h *hmacSignerVerifier) Sign(base []byte) ([]byte, error) {
	mac := hmac.New(sha256.New, h.secret)
	mac.Write(base)

	return mac.Sum(nil), nil
}

func (h *hmacSignerVerifier) Verify(base, sig []byte) error {
	mac := hmac.New(sha256.New, h.secret)
	mac.Write(base)
	expected := mac.Sum(nil)

	if subtle.ConstantTimeCompare(expected, sig) != 1 {
		return ErrInvalidSignature
	}

	return nil
}

// ============================================================================
// Ed25519 (RFC 9421 §3.3.6)
// ============================================================================

type ed25519Signer struct {
	keyID string
	priv  ed25519.PrivateKey
}

// NewEd25519Signer constructs an Ed25519 signer (RFC 9421 §3.3.6).
func NewEd25519Signer(keyID string, priv ed25519.PrivateKey) (Signer, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: invalid ed25519 private key length", ErrKeyMismatch)
	}

	return &ed25519Signer{keyID: keyID, priv: priv}, nil
}

func (e *ed25519Signer) Algorithm() Algorithm { return AlgEd25519 }
func (e *ed25519Signer) KeyID() string        { return e.keyID }

func (e *ed25519Signer) Sign(base []byte) ([]byte, error) {
	return ed25519.Sign(e.priv, base), nil
}

type ed25519Verifier struct {
	keyID string
	pub   ed25519.PublicKey
}

// NewEd25519Verifier constructs an Ed25519 verifier (RFC 9421 §3.3.6).
func NewEd25519Verifier(keyID string, pub ed25519.PublicKey) (Verifier, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: invalid ed25519 public key length", ErrKeyMismatch)
	}

	return &ed25519Verifier{keyID: keyID, pub: pub}, nil
}

func (e *ed25519Verifier) Algorithm() Algorithm { return AlgEd25519 }
func (e *ed25519Verifier) KeyID() string        { return e.keyID }

func (e *ed25519Verifier) Verify(base, sig []byte) error {
	if len(sig) != ed25519.SignatureSize {
		return ErrInvalidSignature
	}

	if !ed25519.Verify(e.pub, base, sig) {
		return ErrInvalidSignature
	}

	return nil
}

// ============================================================================
// ECDSA P-256 and P-384 (RFC 9421 §3.3.4 & §3.3.5)
// ============================================================================

type ecdsaSigner struct {
	keyID string
	alg   Algorithm
	priv  *ecdsa.PrivateKey
}

// NewECDSASigner constructs an ECDSA signer for ecdsa-p256-sha256 or ecdsa-p384-sha384.
func NewECDSASigner(keyID string, alg Algorithm, priv *ecdsa.PrivateKey) (Signer, error) {
	if priv == nil {
		return nil, fmt.Errorf("%w: ecdsa private key cannot be nil", ErrKeyMismatch)
	}

	switch alg {
	case AlgECDSAP256SHA256:
		if priv.Curve != elliptic.P256() {
			return nil, fmt.Errorf("%w: algorithm %s requires P-256 curve", ErrKeyMismatch, alg)
		}
	case AlgECDSAP384SHA384:
		if priv.Curve != elliptic.P384() {
			return nil, fmt.Errorf("%w: algorithm %s requires P-384 curve", ErrKeyMismatch, alg)
		}
	default:
		return nil, fmt.Errorf("%w: %s is not an ECDSA algorithm", ErrUnsupportedAlgorithm, alg)
	}

	return &ecdsaSigner{keyID: keyID, alg: alg, priv: priv}, nil
}

func (e *ecdsaSigner) Algorithm() Algorithm { return e.alg }
func (e *ecdsaSigner) KeyID() string        { return e.keyID }

func (e *ecdsaSigner) Sign(base []byte) ([]byte, error) {
	var (
		digest   []byte
		octetLen int
	)

	switch e.alg {
	case AlgECDSAP256SHA256:
		h := sha256.Sum256(base)
		digest = h[:]
		octetLen = 32
	case AlgECDSAP384SHA384:
		h := sha512.Sum384(base)
		digest = h[:]
		octetLen = 48
	}

	r, s, err := ecdsa.Sign(rand.Reader, e.priv, digest)
	if err != nil {
		return nil, fmt.Errorf("httpsig: ecdsa sign failed: %w", err)
	}

	// Format raw IEEE P1363: zero-padded concatenation of r and s
	sig := make([]byte, octetLen*2)
	r.FillBytes(sig[:octetLen])
	s.FillBytes(sig[octetLen:])

	return sig, nil
}

type ecdsaVerifier struct {
	keyID string
	alg   Algorithm
	pub   *ecdsa.PublicKey
}

// NewECDSAVerifier constructs an ECDSA verifier for ecdsa-p256-sha256 or ecdsa-p384-sha384.
func NewECDSAVerifier(keyID string, alg Algorithm, pub *ecdsa.PublicKey) (Verifier, error) {
	if pub == nil {
		return nil, fmt.Errorf("%w: ecdsa public key cannot be nil", ErrKeyMismatch)
	}

	switch alg {
	case AlgECDSAP256SHA256:
		if pub.Curve != elliptic.P256() {
			return nil, fmt.Errorf("%w: algorithm %s requires P-256 curve", ErrKeyMismatch, alg)
		}
	case AlgECDSAP384SHA384:
		if pub.Curve != elliptic.P384() {
			return nil, fmt.Errorf("%w: algorithm %s requires P-384 curve", ErrKeyMismatch, alg)
		}
	default:
		return nil, fmt.Errorf("%w: %s is not an ECDSA algorithm", ErrUnsupportedAlgorithm, alg)
	}

	return &ecdsaVerifier{keyID: keyID, alg: alg, pub: pub}, nil
}

func (e *ecdsaVerifier) Algorithm() Algorithm { return e.alg }
func (e *ecdsaVerifier) KeyID() string        { return e.keyID }

func (e *ecdsaVerifier) Verify(base, sig []byte) error {
	var (
		digest   []byte
		octetLen int
	)

	switch e.alg {
	case AlgECDSAP256SHA256:
		octetLen = 32
		if len(sig) != octetLen*2 {
			return ErrInvalidSignature
		}

		h := sha256.Sum256(base)
		digest = h[:]

	case AlgECDSAP384SHA384:
		octetLen = 48
		if len(sig) != octetLen*2 {
			return ErrInvalidSignature
		}

		h := sha512.Sum384(base)
		digest = h[:]
	}

	r := new(big.Int).SetBytes(sig[:octetLen])
	s := new(big.Int).SetBytes(sig[octetLen:])

	if !ecdsa.Verify(e.pub, digest, r, s) {
		return ErrInvalidSignature
	}

	return nil
}

// ============================================================================
// RSA (RSASSA-PSS and RSASSA-PKCS1-v1_5) (RFC 9421 §3.3.1 & §3.3.2)
// ============================================================================

type rsaSigner struct {
	keyID string
	alg   Algorithm
	priv  *rsa.PrivateKey
}

// NewRSASigner constructs an RSA signer for rsa-pss-sha512 or rsa-v1_5-sha256.
func NewRSASigner(keyID string, alg Algorithm, priv *rsa.PrivateKey) (Signer, error) {
	if priv == nil {
		return nil, fmt.Errorf("%w: rsa private key cannot be nil", ErrKeyMismatch)
	}

	if alg != AlgRSAPSSSHA512 && alg != AlgRSAPKCS1v15SHA256 {
		return nil, fmt.Errorf("%w: unsupported RSA algorithm %s", ErrUnsupportedAlgorithm, alg)
	}

	return &rsaSigner{keyID: keyID, alg: alg, priv: priv}, nil
}

func (r *rsaSigner) Algorithm() Algorithm { return r.alg }
func (r *rsaSigner) KeyID() string        { return r.keyID }

func (r *rsaSigner) Sign(base []byte) ([]byte, error) {
	switch r.alg {
	case AlgRSAPSSSHA512:
		h := sha512.Sum512(base)
		opts := &rsa.PSSOptions{
			SaltLength: rsa.PSSSaltLengthEqualsHash, // 64 bytes for SHA-512 per RFC 9421 §3.3.1
			Hash:       crypto.SHA512,
		}

		sig, err := rsa.SignPSS(rand.Reader, r.priv, crypto.SHA512, h[:], opts)
		if err != nil {
			return nil, fmt.Errorf("httpsig: rsa-pss sign failed: %w", err)
		}

		return sig, nil

	case AlgRSAPKCS1v15SHA256:
		h := sha256.Sum256(base)

		sig, err := rsa.SignPKCS1v15(rand.Reader, r.priv, crypto.SHA256, h[:])
		if err != nil {
			return nil, fmt.Errorf("httpsig: rsa-v1_5 sign failed: %w", err)
		}

		return sig, nil

	default:
		return nil, ErrUnsupportedAlgorithm
	}
}

type rsaVerifier struct {
	keyID string
	alg   Algorithm
	pub   *rsa.PublicKey
}

// NewRSAVerifier constructs an RSA verifier for rsa-pss-sha512 or rsa-v1_5-sha256.
func NewRSAVerifier(keyID string, alg Algorithm, pub *rsa.PublicKey) (Verifier, error) {
	if pub == nil {
		return nil, fmt.Errorf("%w: rsa public key cannot be nil", ErrKeyMismatch)
	}

	if alg != AlgRSAPSSSHA512 && alg != AlgRSAPKCS1v15SHA256 {
		return nil, fmt.Errorf("%w: unsupported RSA algorithm %s", ErrUnsupportedAlgorithm, alg)
	}

	return &rsaVerifier{keyID: keyID, alg: alg, pub: pub}, nil
}

func (r *rsaVerifier) Algorithm() Algorithm { return r.alg }
func (r *rsaVerifier) KeyID() string        { return r.keyID }

func (r *rsaVerifier) Verify(base, sig []byte) error {
	switch r.alg {
	case AlgRSAPSSSHA512:
		h := sha512.Sum512(base)
		opts := &rsa.PSSOptions{
			SaltLength: rsa.PSSSaltLengthEqualsHash, // 64 bytes for SHA-512 per RFC 9421 §3.3.1
			Hash:       crypto.SHA512,
		}

		if err := rsa.VerifyPSS(r.pub, crypto.SHA512, h[:], sig, opts); err != nil {
			return ErrInvalidSignature
		}

		return nil

	case AlgRSAPKCS1v15SHA256:
		h := sha256.Sum256(base)
		if err := rsa.VerifyPKCS1v15(r.pub, crypto.SHA256, h[:], sig); err != nil {
			return ErrInvalidSignature
		}

		return nil

	default:
		return ErrUnsupportedAlgorithm
	}
}
