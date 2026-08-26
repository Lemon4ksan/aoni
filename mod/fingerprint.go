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

// WithP0fSignature constructs an [RequestModifier] setting p0f TCP stack signature parameters.
func WithP0fSignature(sig *p0f.Signature) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).P0fSignature = sig
	})
}

// WithSessionCache constructs an [RequestModifier] assigning an isolated proxy-aware TLS [fingerprint.SessionCache].
func WithSessionCache(cache fingerprint.SessionCache) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).SessionCache = cache
	})
}

// WithCertificatePin constructs an [RequestModifier] pinning SHA-256 public key hashes for target domains (RFC 7469).
func WithCertificatePin(domain, hash string) RequestModifier {
	return Custom(func(req Request) {
		cfg := getOrInitRequestConfig(req)
		if cfg.CertificatePins == nil {
			cfg.CertificatePins = make(map[string][]string)
		}

		cfg.CertificatePins[domain] = append(cfg.CertificatePins[domain], hash)
	})
}

// WithSPKIPin constructs an [RequestModifier] pinning an SPKI SHA-256 fingerprint hash for target domain (RFC 7469 §2.4).
func WithSPKIPin(domain, pin string) RequestModifier {
	return WithCertificatePin(domain, spki.NormalizePin(pin))
}

// WithPadding constructs an [RequestModifier] injecting random packet padding headers to confuse DPI length analysis.
func WithPadding(cfg fingerprint.PaddingConfig) RequestModifier {
	return Custom(func(req Request) {
		if padding := fingerprint.GeneratePadding(cfg); len(padding) > 0 {
			headerName := fingerprint.PaddingHeaderName(cfg)
			req.SetHeader(headerName, hex.EncodeToString(padding))
		}
	})
}

// WithClientHelloSpecProvider constructs an [RequestModifier] assigning a dynamic uTLS spec provider.
func WithClientHelloSpecProvider(provider fingerprint.ClientHelloSpecProvider) RequestModifier {
	return Custom(func(req Request) {
		getOrInitRequestConfig(req).ClientHelloSpecProvider = provider
	})
}
