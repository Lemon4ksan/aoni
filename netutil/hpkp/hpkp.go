// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package hpkp implements the Public Key Pinning Extension for HTTP strictly conforming to RFC 7469.
// Core implementation is located in [github.com/lemon4ksan/foundation/net/http/hpkp].
package hpkp

import (
	"crypto/x509"
	"net/http"
	"time"

	fhpkp "github.com/lemon4ksan/foundation/net/http/hpkp"
)

// Standard HTTP Header field names defined in RFC 7469 §2.1 & §6.
const (
	HeaderPublicKeyPins           = fhpkp.HeaderPublicKeyPins
	HeaderPublicKeyPinsReportOnly = fhpkp.HeaderPublicKeyPinsReportOnly
)

// Directives defined in RFC 7469 §2.1.
const (
	DirectiveMaxAge            = fhpkp.DirectiveMaxAge
	DirectiveIncludeSubDomains = fhpkp.DirectiveIncludeSubDomains
	DirectiveReportURI         = fhpkp.DirectiveReportURI
	PinPrefixSHA256            = fhpkp.PinPrefixSHA256
)

// Common errors returned by HPKP operations.
var (
	ErrEmptyPinningHeader = fhpkp.ErrEmptyPinningHeader
	ErrMissingMaxAge      = fhpkp.ErrMissingMaxAge
	ErrMissingPins        = fhpkp.ErrMissingPins
	ErrNoMatchingPin      = fhpkp.ErrNoMatchingPin
	ErrNoBackupPin        = fhpkp.ErrNoBackupPin
	ErrInvalidPinFormat   = fhpkp.ErrInvalidPinFormat
	ErrNoCertificates     = fhpkp.ErrNoCertificates
)

// Pin represents an individual Subject Public Key Info (SPKI) fingerprint pin (RFC 7469 §2.4).
type Pin = fhpkp.Pin

// Policy encapsulates a parsed RFC 7469 Public Key Pinning policy.
type Policy = fhpkp.Policy

// ValidationReport represents an RFC 7469 §3 JSON Pin Validation Failure report payload.
type ValidationReport = fhpkp.ValidationReport

// NewPinSHA256 creates a new SHA-256 [Pin] from a base64 or hex-encoded fingerprint string.
func NewPinSHA256(fingerprint string) (Pin, error) {
	return fhpkp.NewPinSHA256(fingerprint)
}

// ComputeSPKIFingerprint calculates the Base64 SHA-256 fingerprint of a certificate's SPKI.
func ComputeSPKIFingerprint(cert *x509.Certificate) string {
	return fhpkp.ComputeSPKIFingerprint(cert)
}

// ComputeSPKIPin returns the formatted `pin-sha256="<base64>"` directive value for cert.
func ComputeSPKIPin(cert *x509.Certificate) string {
	return fhpkp.ComputeSPKIPin(cert)
}

// ComputeSPKIFingerprintFromDER parses a DER-encoded X.509 certificate and returns its Base64 SPKI fingerprint.
func ComputeSPKIFingerprintFromDER(rawCert []byte) (string, error) {
	return fhpkp.ComputeSPKIFingerprintFromDER(rawCert)
}

// ComputeSPKIFingerprintFromSPKI calculates the Base64 SHA-256 fingerprint directly from raw DER-encoded SPKI bytes.
func ComputeSPKIFingerprintFromSPKI(rawSPKI []byte) string {
	return fhpkp.ComputeSPKIFingerprintFromSPKI(rawSPKI)
}

// ParseHeader parses an RFC 7469 Public-Key-Pins or Public-Key-Pins-Report-Only header string into a [Policy].
func ParseHeader(headerValue string) (*Policy, error) {
	return fhpkp.ParseHeader(headerValue)
}

// FromResponse extracts and parses HPKP policies from an [*http.Response].
func FromResponse(resp *http.Response) (*Policy, error) {
	return fhpkp.FromResponse(resp)
}

// FromHeader extracts and parses HPKP policies from an [http.Header].
func FromHeader(header http.Header) (*Policy, error) {
	return fhpkp.FromHeader(header)
}

// BuildValidationReport constructs a standards-compliant JSON failure report per RFC 7469 §3.
func BuildValidationReport(
	hostname string,
	port int,
	notedHostname string,
	policy *Policy,
	servedChain []*x509.Certificate,
	validatedChain []*x509.Certificate,
	observedAt time.Time,
) *ValidationReport {
	return fhpkp.BuildValidationReport(hostname, port, notedHostname, policy, servedChain, validatedChain, observedAt)
}
