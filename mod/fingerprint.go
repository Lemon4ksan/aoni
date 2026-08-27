// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"encoding/hex"

	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
	"github.com/lemon4ksan/aoni/netutil/spki"
)

// WithP0fSignature configures TCP/IP SYN packet fingerprint spoofing (TTL, Window Size, MSS, WScale, ECN) for this request.
func WithP0fSignature(sig *p0f.Signature) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).P0fSignature = sig
	})
}

// WithSessionCache assigns an isolated TLS session cache for TLS 1.2/1.3 session resumption for this request.
func WithSessionCache(cache fingerprint.SessionCache) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).SessionCache = cache
	})
}

// WithCertificatePin pins a SHA-256 certificate public key hash for a target domain (RFC 7469).
func WithCertificatePin(domain, hash string) RequestModifier {
	return Custom(func(req Request) {
		cfg := getOrInitRequestConfig(req)
		if cfg.CertificatePins == nil {
			cfg.CertificatePins = make(map[string][]string)
		}

		cfg.CertificatePins[domain] = append(cfg.CertificatePins[domain], hash)
	})
}

// WithSPKIPin pins an SPKI SHA-256 fingerprint hash for a target domain (RFC 7469 §2.4).
func WithSPKIPin(domain, pin string) RequestModifier {
	return WithCertificatePin(domain, spki.NormalizePin(pin))
}

// WithPadding injects pseudo-random HTTP padding headers to confuse DPI packet length analysis.
func WithPadding(cfg fingerprint.PaddingConfig) RequestModifier {
	return Custom(func(req Request) {
		if padding := fingerprint.GeneratePadding(cfg); len(padding) > 0 {
			headerName := fingerprint.PaddingHeaderName(cfg)
			req.SetHeader(headerName, hex.EncodeToString(padding))
		}
	})
}

// WithClientHelloSpecProvider assigns a dynamic uTLS spec provider callback for customizing the TLS handshake.
func WithClientHelloSpecProvider(provider fingerprint.ClientHelloSpecProvider) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ClientHelloSpecProvider = provider
	})
}
