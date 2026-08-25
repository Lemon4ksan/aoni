// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"time"

	"github.com/lemon4ksan/aoni/netutil/spki"
)

// CertChainInfo holds detailed diagnostic metadata for an inspected TLS certificate chain.
type CertChainInfo struct {
	EarliestExpiry    time.Time
	LastChainExpiry   time.Time
	DaysUntilExpiry   int
	FingerprintSHA256 string
	SPKIFingerprint   string   // Base64 SHA-256 SPKI fingerprint (RFC 7469 §2.4)
	SPKIPin           string   // Formatted pin-sha256="..." directive for leaf certificate (RFC 7469 §2.1.1)
	SPKIPins          []string // Formatted pin-sha256="..." directives for all certificates in chain (RFC 7469 §2.6)
	Subject           string
	Issuer            string
	DNSNames          []string
	SerialNumber      string
	Version           string
	CipherSuite       string
}

// InspectTLSChain extracts certificate chain expiry dates, SHA-256 fingerprints,
// SPKI HPKP fingerprints (RFC 7469), subject/issuer details, and negotiated TLS parameters from a connection state.
func InspectTLSChain(state *tls.ConnectionState) *CertChainInfo {
	if state == nil || len(state.PeerCertificates) == 0 {
		return nil
	}

	leaf := state.PeerCertificates[0]
	earliest := getEarliestCertExpiry(state)
	lastChain := getLastChainExpiry(state)

	fp := sha256.Sum256(leaf.Raw)
	fingerprint := hex.EncodeToString(fp[:])

	spkiFP := spki.ComputeSPKIFingerprint(leaf)
	spkiPin := spki.ComputeSPKIPin(leaf)

	allPins := make([]string, 0, len(state.PeerCertificates))
	for _, cert := range state.PeerCertificates {
		if p := spki.ComputeSPKIPin(cert); p != "" {
			allPins = append(allPins, p)
		}
	}

	daysRemaining := int(time.Until(earliest).Hours() / 24)

	return &CertChainInfo{
		EarliestExpiry:    earliest,
		LastChainExpiry:   lastChain,
		DaysUntilExpiry:   daysRemaining,
		FingerprintSHA256: fingerprint,
		SPKIFingerprint:   spkiFP,
		SPKIPin:           spkiPin,
		SPKIPins:          allPins,
		Subject:           leaf.Subject.String(),
		Issuer:            leaf.Issuer.String(),
		DNSNames:          leaf.DNSNames,
		SerialNumber:      hex.EncodeToString(leaf.SerialNumber.Bytes()),
		Version:           getTLSVersionName(state.Version),
		CipherSuite:       tls.CipherSuiteName(state.CipherSuite),
	}
}

func getEarliestCertExpiry(state *tls.ConnectionState) time.Time {
	var earliest time.Time
	for _, cert := range state.PeerCertificates {
		if (earliest.IsZero() || cert.NotAfter.Before(earliest)) && !cert.NotAfter.IsZero() {
			earliest = cert.NotAfter
		}
	}

	return earliest
}

func getLastChainExpiry(state *tls.ConnectionState) time.Time {
	var lastChainExpiry time.Time
	for _, chain := range state.VerifiedChains {
		var earliestCertExpiry time.Time
		for _, cert := range chain {
			if (earliestCertExpiry.IsZero() || cert.NotAfter.Before(earliestCertExpiry)) && !cert.NotAfter.IsZero() {
				earliestCertExpiry = cert.NotAfter
			}
		}

		if lastChainExpiry.IsZero() || lastChainExpiry.Before(earliestCertExpiry) {
			lastChainExpiry = earliestCertExpiry
		}
	}

	return lastChainExpiry
}

// getTLSVersionName maps the binary TLS version number to a human-readable protocol label (RFC 8996 / RFC 8446 / RFC 9846).
func getTLSVersionName(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0" // Deprecated per RFC 8996 (Historic)
	case tls.VersionTLS11:
		return "TLS 1.1" // Deprecated per RFC 8996 (Historic)
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return "Unknown"
	}
}
