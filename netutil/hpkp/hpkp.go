// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package hpkp implements the Public Key Pinning Extension for HTTP strictly conforming to RFC 7469.
//
// It provides Subject Public Key Info (SPKI) SHA-256 fingerprint generation, Public-Key-Pins and
// Public-Key-Pins-Report-Only response header parsing, certificate chain validation, backup pin verification,
// and JSON failure report generation per RFC 7469 §3.
package hpkp

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Standard HTTP Header field names defined in RFC 7469 §2.1 & §6.
const (
	// HeaderPublicKeyPins is the standard response header for enforcing Public Key Pinning (RFC 7469 §2.1).
	HeaderPublicKeyPins = "Public-Key-Pins"

	// HeaderPublicKeyPinsReportOnly is the response header for reporting-only Public Key Pinning (RFC 7469 §2.1).
	HeaderPublicKeyPinsReportOnly = "Public-Key-Pins-Report-Only"
)

// Directives defined in RFC 7469 §2.1.
const (
	// DirectiveMaxAge is the required max-age directive specifying TTL in seconds (RFC 7469 §2.1.2).
	DirectiveMaxAge = "max-age"

	// DirectiveIncludeSubDomains indicates that pinning applies to all subdomains (RFC 7469 §2.1.3).
	DirectiveIncludeSubDomains = "includeSubDomains"

	// DirectiveReportURI specifies the URI for POSTing failure reports (RFC 7469 §2.1.4).
	DirectiveReportURI = "report-uri"

	// PinPrefixSHA256 is the standard prefix for SHA-256 SPKI fingerprint directives (RFC 7469 §2.1.1).
	PinPrefixSHA256 = "pin-sha256"
)

// Common errors returned by HPKP operations.
var (
	// ErrEmptyPinningHeader indicates that the provided header string is empty.
	ErrEmptyPinningHeader = errors.New("hpkp: empty public key pinning header")

	// ErrMissingMaxAge indicates that the required max-age directive was not found in Public-Key-Pins (RFC 7469 §2.1.2).
	ErrMissingMaxAge = errors.New("hpkp: missing required max-age directive (RFC 7469 §2.1.2)")

	// ErrMissingPins indicates that no valid pin directives were found (RFC 7469 §2.1.1).
	ErrMissingPins = errors.New("hpkp: missing pin directives (RFC 7469 §2.1.1)")

	// ErrNoMatchingPin indicates that the presented certificate chain does not intersect with any pinned key (RFC 7469 §2.6).
	ErrNoMatchingPin = errors.New("hpkp: certificate chain does not match any pinned public key (RFC 7469 §2.6)")

	// ErrNoBackupPin indicates that the policy does not include a backup pin outside the live chain (RFC 7469 §2.5).
	ErrNoBackupPin = errors.New(
		"hpkp: policy lacks a required backup pin outside the active certificate chain (RFC 7469 §2.5)",
	)

	// ErrInvalidPinFormat indicates that an SPKI pin string could not be decoded as base64 or hex.
	ErrInvalidPinFormat = errors.New("hpkp: invalid SPKI pin fingerprint format")

	// ErrNoCertificates indicates that an empty certificate chain was provided for validation.
	ErrNoCertificates = errors.New("hpkp: no certificates provided for pin validation")
)

// Pin represents an individual Subject Public Key Info (SPKI) fingerprint pin (RFC 7469 §2.4).
type Pin struct {
	// Algorithm specifies the cryptographic hash algorithm (e.g. "sha256").
	Algorithm string
	// Fingerprint is the Base64-encoded digest of the SubjectPublicKeyInfo DER representation.
	Fingerprint string
	// RawBytes contains the decoded binary hash bytes (32 bytes for SHA-256).
	RawBytes []byte
}

// NewPinSHA256 creates a new SHA-256 [Pin] from a base64 or hex-encoded fingerprint string.
func NewPinSHA256(fingerprint string) (Pin, error) {
	raw, err := parsePinBytes(fingerprint)
	if err != nil {
		return Pin{}, err
	}

	b64 := base64.StdEncoding.EncodeToString(raw)

	return Pin{
		Algorithm:   "sha256",
		Fingerprint: b64,
		RawBytes:    raw,
	}, nil
}

// String returns the directive string representation (e.g. `pin-sha256="d6qzRu9zOECb90Uez27xWltNsj0e1Md7GkYYkVoZWmM="`).
func (p Pin) String() string {
	algo := p.Algorithm
	if algo == "" {
		algo = "sha256"
	}

	return fmt.Sprintf("pin-%s=%q", algo, p.Fingerprint)
}

// Matches returns true if the pin matches the given SHA-256 SPKI hash bytes.
func (p Pin) Matches(spkiHash []byte) bool {
	return bytes.Equal(p.RawBytes, spkiHash)
}

// Policy encapsulates a parsed RFC 7469 Public Key Pinning policy.
type Policy struct {
	// Pins contains all parsed SPKI fingerprint pins.
	Pins []Pin
	// MaxAge specifies the policy lifetime (TTL) in seconds (RFC 7469 §2.1.2).
	MaxAge time.Duration
	// IncludeSubDomains indicates whether subdomains inherit this pinning policy (RFC 7469 §2.1.3).
	IncludeSubDomains bool
	// ReportURI is the optional target endpoint for posting violation reports (RFC 7469 §2.1.4).
	ReportURI string
	// ReportOnly indicates whether this policy was received via Public-Key-Pins-Report-Only.
	ReportOnly bool
}

// String formats the policy into an RFC 7469 compliant header value.
func (p *Policy) String() string {
	var sb strings.Builder

	if !p.ReportOnly {
		sb.WriteString("max-age=")
		sb.WriteString(strconv.FormatInt(int64(p.MaxAge.Seconds()), 10))
	}

	for _, pin := range p.Pins {
		if sb.Len() > 0 {
			sb.WriteString("; ")
		}

		sb.WriteString(pin.String())
	}

	if p.IncludeSubDomains {
		if sb.Len() > 0 {
			sb.WriteString("; ")
		}

		sb.WriteString(DirectiveIncludeSubDomains)
	}

	if p.ReportURI != "" {
		if sb.Len() > 0 {
			sb.WriteString("; ")
		}

		fmt.Fprintf(&sb, "%s=%q", DirectiveReportURI, p.ReportURI)
	}

	return sb.String()
}

// HasPin returns true if the policy contains a pin matching the given base64 fingerprint or hash bytes.
func (p *Policy) HasPin(fingerprint string) bool {
	raw, err := parsePinBytes(fingerprint)
	if err != nil {
		return false
	}

	for _, pin := range p.Pins {
		if pin.Matches(raw) {
			return true
		}
	}

	return false
}

// ValidateChain validates that at least one certificate in the provided X.509 chain matches a pinned SPKI (RFC 7469 §2.6).
func (p *Policy) ValidateChain(chain []*x509.Certificate) error {
	if len(chain) == 0 {
		return ErrNoCertificates
	}

	if len(p.Pins) == 0 {
		return nil
	}

	for _, cert := range chain {
		if cert == nil || len(cert.RawSubjectPublicKeyInfo) == 0 {
			continue
		}

		spkiHash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
		for _, pin := range p.Pins {
			if pin.Matches(spkiHash[:]) {
				return nil
			}
		}
	}

	return ErrNoMatchingPin
}

// ValidateRawCerts validates that at least one DER-encoded certificate matches a pinned SPKI (RFC 7469 §2.6).
func (p *Policy) ValidateRawCerts(rawCerts [][]byte) error {
	if len(rawCerts) == 0 {
		return ErrNoCertificates
	}

	if len(p.Pins) == 0 {
		return nil
	}

	for _, raw := range rawCerts {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			continue
		}

		spkiHash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
		for _, pin := range p.Pins {
			if pin.Matches(spkiHash[:]) {
				return nil
			}
		}
	}

	return ErrNoMatchingPin
}

// ValidateNotingCriteria checks whether this policy meets the three mandatory RFC 7469 §2.5 criteria:
// 1. Certificate chain is non-empty and valid.
// 2. At least one pin matches an SPKI in the live chain.
// 3. At least one backup pin does NOT match any SPKI in the live chain.
func (p *Policy) ValidateNotingCriteria(chain []*x509.Certificate) error {
	if len(chain) == 0 {
		return ErrNoCertificates
	}

	if len(p.Pins) < 2 {
		return ErrNoBackupPin
	}

	chainHashes := make([][32]byte, 0, len(chain))
	for _, cert := range chain {
		if cert != nil && len(cert.RawSubjectPublicKeyInfo) > 0 {
			chainHashes = append(chainHashes, sha256.Sum256(cert.RawSubjectPublicKeyInfo))
		}
	}

	var (
		hasMatchingPin bool
		hasBackupPin   bool
	)

	for _, pin := range p.Pins {
		matched := false
		for _, ch := range chainHashes {
			if bytes.Equal(pin.RawBytes, ch[:]) {
				matched = true
				break
			}
		}

		if matched {
			hasMatchingPin = true
		} else {
			hasBackupPin = true
		}
	}

	if !hasMatchingPin {
		return ErrNoMatchingPin
	}

	if !hasBackupPin {
		return ErrNoBackupPin
	}

	return nil
}

// ComputeSPKIFingerprint calculates the Base64 SHA-256 fingerprint of a certificate's SPKI (RFC 7469 §2.4 & Appendix A).
func ComputeSPKIFingerprint(cert *x509.Certificate) string {
	if cert == nil || len(cert.RawSubjectPublicKeyInfo) == 0 {
		return ""
	}

	hash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)

	return base64.StdEncoding.EncodeToString(hash[:])
}

// ComputeSPKIPin returns the formatted `pin-sha256="<base64>"` directive value for cert (RFC 7469 §2.1.1).
func ComputeSPKIPin(cert *x509.Certificate) string {
	fp := ComputeSPKIFingerprint(cert)
	if fp == "" {
		return ""
	}

	return fmt.Sprintf("pin-sha256=%q", fp)
}

// ComputeSPKIFingerprintFromDER parses a DER-encoded X.509 certificate and returns its Base64 SPKI fingerprint.
func ComputeSPKIFingerprintFromDER(rawCert []byte) (string, error) {
	cert, err := x509.ParseCertificate(rawCert)
	if err != nil {
		return "", fmt.Errorf("hpkp: failed to parse certificate DER: %w", err)
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

// ParseHeader parses an RFC 7469 Public-Key-Pins or Public-Key-Pins-Report-Only header string into a [Policy].
func ParseHeader(headerValue string) (*Policy, error) {
	headerValue = strings.TrimSpace(headerValue)
	if headerValue == "" {
		return nil, ErrEmptyPinningHeader
	}

	policy := &Policy{
		Pins: make([]Pin, 0, 4),
	}

	var hasMaxAge bool

	directives := strings.Split(headerValue, ";")

	for _, rawDir := range directives {
		dir := strings.TrimSpace(rawDir)
		if dir == "" {
			continue
		}

		key, val, found := strings.Cut(dir, "=")
		key = strings.TrimSpace(key)
		lowerKey := strings.ToLower(key)

		if !found {
			if strings.EqualFold(key, DirectiveIncludeSubDomains) {
				policy.IncludeSubDomains = true
			}

			continue
		}

		val = strings.TrimSpace(val)
		val = strings.Trim(val, "\"")

		switch {
		case lowerKey == DirectiveMaxAge:
			seconds, err := strconv.ParseInt(val, 10, 64)
			if err == nil && seconds >= 0 {
				policy.MaxAge = time.Duration(seconds) * time.Second
				hasMaxAge = true
			}

		case lowerKey == DirectiveReportURI:
			policy.ReportURI = val

		case strings.HasPrefix(lowerKey, "pin-"):
			algo := strings.TrimPrefix(lowerKey, "pin-")
			if algo == "sha256" {
				pin, err := NewPinSHA256(val)
				if err == nil {
					policy.Pins = append(policy.Pins, pin)
				}
			}
		}
	}

	if len(policy.Pins) == 0 {
		return nil, ErrMissingPins
	}

	if !policy.ReportOnly && !hasMaxAge {
		return nil, ErrMissingMaxAge
	}

	return policy, nil
}

// FromResponse extracts and parses HPKP policies from an [*http.Response] (RFC 7469 §2.3).
func FromResponse(resp *http.Response) (*Policy, error) {
	if resp == nil || resp.Header == nil {
		return nil, ErrEmptyPinningHeader
	}

	return FromHeader(resp.Header)
}

// FromHeader extracts and parses HPKP policies from an [http.Header] (RFC 7469 §2.3).
// If both Public-Key-Pins and Public-Key-Pins-Report-Only headers are present, Public-Key-Pins takes precedence.
func FromHeader(header http.Header) (*Policy, error) {
	if header == nil {
		return nil, ErrEmptyPinningHeader
	}

	if val := header.Get(HeaderPublicKeyPins); val != "" {
		p, err := ParseHeader(val)
		if err == nil {
			p.ReportOnly = false
			return p, nil
		}

		return nil, err
	}

	if val := header.Get(HeaderPublicKeyPinsReportOnly); val != "" {
		p, err := ParseHeader(val)
		if err == nil {
			p.ReportOnly = true
			return p, nil
		}

		return nil, err
	}

	return nil, ErrEmptyPinningHeader
}

// ValidationReport represents an RFC 7469 §3 JSON Pin Validation Failure report payload.
type ValidationReport struct {
	// DateTime indicates the time the client observed the pin validation failure (RFC 3339 format).
	DateTime string `json:"date-time"`

	// Hostname is the host to which the connection attempt failed.
	Hostname string `json:"hostname"`

	// Port is the destination port of the failed connection attempt.
	Port int `json:"port"`

	// EffectiveExpirationDate is the RFC 3339 formatted expiration timestamp for the noted policy.
	EffectiveExpirationDate string `json:"effective-expiration-date"`

	// IncludeSubdomains indicates whether includeSubDomains was asserted.
	IncludeSubdomains bool `json:"include-subdomains"`

	// NotedHostname is the origin domain where pins were originally noted.
	NotedHostname string `json:"noted-hostname,omitempty"`

	// ServedCertificateChain contains PEM-encoded certificates served during TLS setup.
	ServedCertificateChain []string `json:"served-certificate-chain"`

	// ValidatedCertificateChain contains PEM-encoded certificates constructed during verification.
	ValidatedCertificateChain []string `json:"validated-certificate-chain"`

	// KnownPins contains array of expected pin directives (e.g. `pin-sha256="..."`).
	KnownPins []string `json:"known-pins"`
}

// JSON serializes the validation report to an indented JSON byte slice.
func (r *ValidationReport) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
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
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}

	var (
		expirationStr     string
		includeSubDomains bool
	)

	knownPins := make([]string, 0)

	if policy != nil {
		if policy.MaxAge > 0 {
			exp := observedAt.Add(policy.MaxAge)
			expirationStr = exp.Format(time.RFC3339)
		}

		includeSubDomains = policy.IncludeSubDomains

		for _, pin := range policy.Pins {
			knownPins = append(knownPins, pin.String())
		}
	}

	servedPEMs := make([]string, 0, len(servedChain))
	for _, cert := range servedChain {
		if cert != nil && len(cert.Raw) > 0 {
			block := &pem.Block{
				Type:  "CERTIFICATE",
				Bytes: cert.Raw,
			}
			servedPEMs = append(servedPEMs, strings.TrimSpace(string(pem.EncodeToMemory(block))))
		}
	}

	validatedPEMs := make([]string, 0, len(validatedChain))
	for _, cert := range validatedChain {
		if cert != nil && len(cert.Raw) > 0 {
			block := &pem.Block{
				Type:  "CERTIFICATE",
				Bytes: cert.Raw,
			}
			validatedPEMs = append(validatedPEMs, strings.TrimSpace(string(pem.EncodeToMemory(block))))
		}
	}

	return &ValidationReport{
		DateTime:                  observedAt.Format(time.RFC3339),
		Hostname:                  hostname,
		Port:                      port,
		EffectiveExpirationDate:   expirationStr,
		IncludeSubdomains:         includeSubDomains,
		NotedHostname:             notedHostname,
		ServedCertificateChain:    servedPEMs,
		ValidatedCertificateChain: validatedPEMs,
		KnownPins:                 knownPins,
	}
}

func parsePinBytes(pin string) ([]byte, error) {
	pin = strings.TrimSpace(pin)
	lower := strings.ToLower(pin)

	if strings.HasPrefix(lower, "pin-sha256=") {
		pin = strings.TrimPrefix(pin, "pin-sha256=")
		pin = strings.Trim(pin, "\"")
	} else if strings.HasPrefix(lower, "sha256/") {
		pin = pin[7:]
	}

	pin = strings.Trim(pin, "\"")

	if b, err := base64.StdEncoding.DecodeString(pin); err == nil && len(b) == 32 {
		return b, nil
	}

	if b, err := base64.RawStdEncoding.DecodeString(pin); err == nil && len(b) == 32 {
		return b, nil
	}

	if b, err := hex.DecodeString(pin); err == nil && len(b) == 32 {
		return b, nil
	}

	return nil, ErrInvalidPinFormat
}
