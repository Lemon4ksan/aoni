// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package httpsig

import (
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Standard RFC 9530 Digest Algorithm Identifiers (§2).
const (
	// DigestSHA256 represents SHA-256 digest format (sha-256).
	DigestSHA256 = "sha-256"

	// DigestSHA512 represents SHA-512 digest format (sha-512).
	DigestSHA512 = "sha-512"
)

// ErrDigestMismatch is returned when the actual payload digest does not match the Content-Digest header value.
var ErrDigestMismatch = errors.New("httpsig: content-digest mismatch")

// ComputeContentDigest calculates the RFC 9530 Content-Digest header value for the given body bytes.
// If no algorithms are specified, "sha-256" is used by default.
// Example output: `sha-256=:X48E9qOokqqrvdts8nOJRJN3OWDUoyWxBf7kbu9DBPE=:`
func ComputeContentDigest(body []byte, algs ...string) string {
	if len(algs) == 0 {
		algs = []string{DigestSHA256}
	}

	var parts []string
	for _, alg := range algs {
		switch strings.ToLower(alg) {
		case DigestSHA256, "sha256":
			h := sha256.Sum256(body)
			enc := base64.StdEncoding.EncodeToString(h[:])
			parts = append(parts, DigestSHA256+"=:"+enc+":")

		case DigestSHA512, "sha512":
			h := sha512.Sum512(body)
			enc := base64.StdEncoding.EncodeToString(h[:])
			parts = append(parts, DigestSHA512+"=:"+enc+":")
		}
	}

	if len(parts) == 0 {
		h := sha256.Sum256(body)
		enc := base64.StdEncoding.EncodeToString(h[:])
		return DigestSHA256 + "=:" + enc + ":"
	}

	return strings.Join(parts, ", ")
}

// VerifyContentDigest checks whether body matches the hashes declared in a Content-Digest or Repr-Digest header (RFC 9530 §2).
func VerifyContentDigest(body []byte, headerVal string) error {
	headerVal = strings.TrimSpace(headerVal)
	if headerVal == "" {
		return errors.New("httpsig: empty content-digest header")
	}

	items := strings.Split(headerVal, ",")
	verifiedAny := false

	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		eqIdx := strings.IndexByte(item, '=')
		if eqIdx == -1 {
			continue
		}

		alg := strings.ToLower(strings.TrimSpace(item[:eqIdx]))
		val := strings.TrimSpace(item[eqIdx+1:])

		// Strip byte-sequence colon wrappers (:...:)
		if strings.HasPrefix(val, ":") && strings.HasSuffix(val, ":") && len(val) >= 2 {
			val = val[1 : len(val)-1]
		}

		expectedBytes, err := base64.StdEncoding.DecodeString(val)
		if err != nil {
			return fmt.Errorf("%w: invalid base64 in digest %q: %w", ErrInvalidStructuredField, val, err)
		}

		var actualBytes []byte
		switch alg {
		case DigestSHA256:
			h := sha256.Sum256(body)
			actualBytes = h[:]
		case DigestSHA512:
			h := sha512.Sum512(body)
			actualBytes = h[:]
		default:
			// Ignore unsupported algorithms if at least one standard algorithm succeeds
			continue
		}

		if subtle.ConstantTimeCompare(actualBytes, expectedBytes) != 1 {
			return ErrDigestMismatch
		}

		verifiedAny = true
	}

	if !verifiedAny {
		return fmt.Errorf("%w: no supported digest algorithm found in %q", ErrUnsupportedAlgorithm, headerVal)
	}

	return nil
}
