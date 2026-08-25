// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package spki provides zero-allocation calculation and verification of Subject Public Key Info (SPKI)
// fingerprints strictly conforming to RFC 7469 §2.4 for client-side TLS Certificate Pinning.
package spki

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"strings"
)

// PinPrefixSHA256 specifies the standard RFC 7469 §2.1.1 pin-sha256 directive prefix.
const PinPrefixSHA256 = "pin-sha256="

// ErrNoCertificates is returned when verifying SPKI pins on an empty peer certificate list.
var ErrNoCertificates = errors.New("aoni/spki: peer certificates slice is empty")

// ComputeSPKIFingerprint calculates the Base64 SHA-256 fingerprint of a certificate's SPKI (RFC 7469 §2.4).
func ComputeSPKIFingerprint(cert *x509.Certificate) string {
	if cert == nil || len(cert.RawSubjectPublicKeyInfo) == 0 {
		return ""
	}

	return ComputeSPKIFingerprintFromSPKI(cert.RawSubjectPublicKeyInfo)
}

// ComputeSPKIPin returns the formatted RFC 7469 §2.1.1 `pin-sha256="<base64>"` directive value for cert.
func ComputeSPKIPin(cert *x509.Certificate) string {
	fp := ComputeSPKIFingerprint(cert)
	if fp == "" {
		return ""
	}

	return `pin-sha256="` + fp + `"`
}

// ComputeSPKIFingerprintFromDER parses a DER-encoded X.509 certificate and returns its Base64 SPKI fingerprint.
func ComputeSPKIFingerprintFromDER(rawCert []byte) (string, error) {
	cert, err := x509.ParseCertificate(rawCert)
	if err != nil {
		return "", err
	}

	return ComputeSPKIFingerprint(cert), nil
}

// ComputeSPKIFingerprintFromSPKI calculates the Base64 SHA-256 fingerprint directly from raw DER-encoded SPKI bytes.
func ComputeSPKIFingerprintFromSPKI(rawSPKI []byte) string {
	if len(rawSPKI) == 0 {
		return ""
	}

	hash := sha256.Sum256(rawSPKI)

	return base64.StdEncoding.EncodeToString(hash[:])
}

// NormalizePin strips optional `pin-sha256=` prefix and surrounding double quotes from rawPin.
func NormalizePin(rawPin string) string {
	pin := strings.TrimSpace(rawPin)
	pin = strings.TrimPrefix(pin, PinPrefixSHA256)
	pin = strings.Trim(pin, `"`)

	return pin
}

// VerifySPKIPins validates whether at least one certificate in the TLS connection's peer chain
// matches one of the expected Base64 SPKI fingerprints (RFC 7469 §2.6).
func VerifySPKIPins(state *tls.ConnectionState, expectedPins []string) bool {
	if state == nil || len(state.PeerCertificates) == 0 || len(expectedPins) == 0 {
		return false
	}

	normalized := make([]string, 0, len(expectedPins))
	for _, p := range expectedPins {
		if norm := NormalizePin(p); norm != "" {
			normalized = append(normalized, norm)
		}
	}

	if len(normalized) == 0 {
		return false
	}

	for _, cert := range state.PeerCertificates {
		spki := ComputeSPKIFingerprint(cert)
		if spki == "" {
			continue
		}

		for _, expected := range normalized {
			if spki == expected {
				return true
			}
		}
	}

	return false
}
