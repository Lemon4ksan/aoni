// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// matchHost checks if host matches pinDomain, supporting wildcards like "*.example.com".
func matchHost(host, pinDomain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	pinDomain = strings.ToLower(strings.TrimSpace(pinDomain))

	if host == pinDomain {
		return true
	}

	if strings.HasPrefix(pinDomain, "*.") {
		base := pinDomain[2:]
		parts := strings.Split(host, ".")

		pinParts := strings.Split(pinDomain, ".")
		if len(parts) == len(pinParts) {
			match := true
			for i := 1; i < len(pinParts); i++ {
				if parts[i] != pinParts[i] {
					match = false
					break
				}
			}

			if match {
				return true
			}
		}

		// Also handle case where wildcard is configured but host is just
		// the base domain, e.g. *.example.com matching example.com
		if host == base {
			return true
		}
	}

	return false
}

// parsePin decodes a SHA-256 fingerprint hash from a string.
func parsePin(pin string) ([]byte, error) {
	pin = strings.TrimSpace(pin)
	if strings.HasPrefix(strings.ToLower(pin), "sha256/") {
		pin = pin[7:]
	}

	if b, err := base64.StdEncoding.DecodeString(pin); err == nil && len(b) == 32 {
		return b, nil
	}

	if b, err := base64.RawStdEncoding.DecodeString(pin); err == nil && len(b) == 32 {
		return b, nil
	}

	if b, err := hex.DecodeString(pin); err == nil && len(b) == 32 {
		return b, nil
	}

	return nil, errors.New("invalid pin format: must be 32-byte sha256 hash in base64 or hex")
}

// verifyCertificatePins verifies the certificate chain against the configured pins for the target host.
func verifyCertificatePins(host string, pins map[string][]string, rawCerts [][]byte) error {
	if len(rawCerts) == 0 {
		return errors.New("aoni: no certificates presented by peer")
	}

	// Find if there are pins configured for this host (directly or via wildcards)
	var hostPins []string
	for pinDomain, domainPins := range pins {
		if matchHost(host, pinDomain) {
			hostPins = append(hostPins, domainPins...)
		}
	}

	// If no pins are configured for this host, succeed (pinning is not active for this host)
	if len(hostPins) == 0 {
		return nil
	}

	// Parse the pins into raw 32-byte sha256 hashes
	var expectedHashes [][]byte
	for _, p := range hostPins {
		hashBytes, err := parsePin(p)
		if err != nil {
			return fmt.Errorf("aoni: failed to parse certificate pin %q: %w", p, err)
		}

		expectedHashes = append(expectedHashes, hashBytes)
	}

	for _, rawCert := range rawCerts {
		cert, err := x509.ParseCertificate(rawCert)
		if err != nil {
			return fmt.Errorf("aoni: failed to parse peer certificate: %w", err)
		}

		sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)

		for _, expected := range expectedHashes {
			if bytes.Equal(sum[:], expected) {
				return nil
			}
		}
	}

	return ErrCertificatePinning
}
