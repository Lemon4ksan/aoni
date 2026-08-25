// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package dpop implements RFC 9449 OAuth 2.0 Demonstrating Proof-of-Possession (DPoP)
// and RFC 7638 JSON Web Key (JWK) Thumbprints.
//
// DPoP provides an application-level mechanism for sender-constraining OAuth 2.0 access
// and refresh tokens, preventing token theft and replay attacks.
//
// # Supported Key Types & Algorithms (RFC 9449 §4.2 & RFC 7518)
//
//   - ECDSA P-256 (ES256)
//   - ECDSA P-384 (ES384)
//   - Ed25519 / OKP (EdDSA)
//   - RSA (RS256, PS256)
//
// # Key Features
//
//   - Proof JWT construction with "typ": "dpop+jwt", embedded public JWK, jti, htm, htu, iat, ath, and nonce.
//   - Access Token Hash ("ath") calculation: base64url(sha256(access_token)).
//   - Canonical RFC 7638 JWK Thumbprint calculation ("jkt") for token introspection and confirmation ("cnf").
//   - Full verification suite for authorization servers and resource servers.
package dpop
