// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webpush

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"
)

// VAPIDKeys encapsulates an ECDSA NIST P-256 key pair for VAPID authentication (RFC 8292).
type VAPIDKeys struct {
	PrivateKey *ecdsa.PrivateKey
	PublicKey  *ecdsa.PublicKey
}

// GenerateVAPIDKeys generates a fresh ECDSA P-256 key pair for VAPID signing.
func GenerateVAPIDKeys() (*VAPIDKeys, error) {
	curve := elliptic.P256()
	priv, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("aoni/webpush: failed to generate VAPID key: %w", err)
	}

	return &VAPIDKeys{
		PrivateKey: priv,
		PublicKey:  &priv.PublicKey,
	}, nil
}

// NewVAPIDKeys parses base64url-encoded private and public keys.
func NewVAPIDKeys(privateKeyBase64, publicKeyBase64 string) (*VAPIDKeys, error) {
	privBytes, err := decodeBase64URL(privateKeyBase64)
	if err != nil {
		return nil, ErrInvalidVAPIDKeys
	}

	return NewVAPIDKeysFromRaw(privBytes)
}

// NewVAPIDKeysFromRaw constructs [VAPIDKeys] from a 32-byte scalar private key.
func NewVAPIDKeysFromRaw(privBytes []byte) (*VAPIDKeys, error) {
	if len(privBytes) != 32 {
		return nil, ErrInvalidVAPIDKeys
	}

	curve := elliptic.P256()
	d := new(big.Int).SetBytes(privBytes)

	x, y := curve.ScalarBaseMult(privBytes)
	if x == nil || y == nil {
		return nil, ErrInvalidVAPIDKeys
	}

	priv := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: curve,
			X:     x,
			Y:     y,
		},
		D: d,
	}

	return &VAPIDKeys{
		PrivateKey: priv,
		PublicKey:  &priv.PublicKey,
	}, nil
}

// PublicKeyBase64 returns the uncompressed 65-byte P-256 public key encoded in base64url (RFC 8292 §3.2).
func (k *VAPIDKeys) PublicKeyBase64() string {
	if k == nil || k.PublicKey == nil {
		return ""
	}

	raw := elliptic.Marshal(elliptic.P256(), k.PublicKey.X, k.PublicKey.Y)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// PrivateKeyBase64 returns the 32-byte scalar private key encoded in base64url.
func (k *VAPIDKeys) PrivateKeyBase64() string {
	if k == nil || k.PrivateKey == nil || k.PrivateKey.D == nil {
		return ""
	}

	var dBytes [32]byte
	k.PrivateKey.D.FillBytes(dBytes[:])

	return base64.RawURLEncoding.EncodeToString(dBytes[:])
}

type jwtHeader struct {
	Typ string `json:"typ"`
	Alg string `json:"alg"`
}

type jwtClaims struct {
	Aud string `json:"aud"`
	Exp int64  `json:"exp"`
	Sub string `json:"sub,omitempty"`
}

// SignJWT constructs and signs an RFC 8292 VAPID JSON Web Token.
func (k *VAPIDKeys) SignJWT(endpoint string, subject string, ttl time.Duration) (string, error) {
	if k == nil || k.PrivateKey == nil {
		return "", ErrInvalidVAPIDKeys
	}

	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", ErrInvalidSubscription
	}

	aud := u.Scheme + "://" + u.Host

	if ttl <= 0 || ttl > 24*time.Hour {
		ttl = 12 * time.Hour
	}

	exp := time.Now().Add(ttl).Unix()

	headerBytes, err := json.Marshal(jwtHeader{Typ: "JWT", Alg: "ES256"})
	if err != nil {
		return "", err
	}

	claimsBytes, err := json.Marshal(jwtClaims{
		Aud: aud,
		Exp: exp,
		Sub: subject,
	})
	if err != nil {
		return "", err
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerBytes)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsBytes)
	signingInput := headerB64 + "." + claimsB64

	hashed := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, k.PrivateKey, hashed[:])
	if err != nil {
		return "", fmt.Errorf("aoni/webpush: failed to sign VAPID JWT: %w", err)
	}

	// ES256 raw signature: 32 bytes R || 32 bytes S
	sigBytes := make([]byte, 64)
	r.FillBytes(sigBytes[:32])
	s.FillBytes(sigBytes[32:])

	sigB64 := base64.RawURLEncoding.EncodeToString(sigBytes)

	return signingInput + "." + sigB64, nil
}

// AuthorizationHeader generates the RFC 8292 §3 `Authorization: vapid t=<JWT>, k=<PublicKey>` header value.
func (k *VAPIDKeys) AuthorizationHeader(endpoint string, subject string, ttl time.Duration) (string, error) {
	jwtToken, err := k.SignJWT(endpoint, subject, ttl)
	if err != nil {
		return "", err
	}

	pubKeyB64 := k.PublicKeyBase64()
	return fmt.Sprintf("vapid t=%s, k=%s", jwtToken, pubKeyB64), nil
}

func decodeBase64URL(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}

	return base64.URLEncoding.DecodeString(s)
}
