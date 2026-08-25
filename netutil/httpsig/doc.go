// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package httpsig implements RFC 9421 (HTTP Message Signatures) and RFC 9530 (Digest Fields).
//
// RFC 9421 establishes a standardized, end-to-end cryptographic signature mechanism
// over components of HTTP request and response messages (@method, @authority, @path,
// @query, @query-param, @target-uri, @scheme, @request-target, @status, Content-Digest,
// and arbitrary HTTP header fields).
//
// # Supported Signature Algorithms (RFC 9421 §6.2)
//
//   - rsa-pss-sha512: RSASSA-PSS using SHA-512 (MGF1 SHA-512, salt length 64 bytes)
//   - rsa-v1_5-sha256: RSASSA-PKCS1-v1_5 using SHA-256
//   - hmac-sha256: HMAC using SHA-256
//   - ecdsa-p256-sha256: ECDSA using NIST P-256 with SHA-256 (raw IEEE P1363 64-byte signature)
//   - ecdsa-p384-sha384: ECDSA using NIST P-384 with SHA-384 (raw IEEE P1363 96-byte signature)
//   - ed25519: EdDSA using Curve edwards25519 (raw 64-byte signature)
//
// # Digest Fields (RFC 9530)
//
// To sign message payloads, RFC 9530 defines the "Content-Digest" and "Repr-Digest" headers
// (e.g. "Content-Digest: sha-256=:X48E9qOokqqrvdts8nOJRJN3OWDUoyWxBf7kbu9DBPE=:").
package httpsig
