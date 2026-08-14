// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mod

import (
	"encoding/hex"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/p0f"
)

// WithP0fSignature constructs an [aoni.RequestModifier] setting p0f TCP stack signature parameters.
func WithP0fSignature(sig *p0f.Signature) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).P0fSignature = sig
	})
}

// WithSessionCache constructs an [aoni.RequestModifier] assigning an isolated proxy-aware TLS [aoni.SessionCache].
func WithSessionCache(cache aoni.SessionCache) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).SessionCache = cache
	})
}

// WithCertificatePin constructs an [aoni.RequestModifier] pinning SHA-256 public key hashes for target domains.
func WithCertificatePin(domain, hash string) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		cfg := aoni.GetOrInitRequestConfig(req)
		if cfg.CertificatePins == nil {
			cfg.CertificatePins = make(map[string][]string)
		}

		cfg.CertificatePins[domain] = append(cfg.CertificatePins[domain], hash)
	})
}

// WithPadding constructs an [aoni.RequestModifier] injecting random packet padding headers to confuse DPI length analysis.
func WithPadding(cfg fingerprint.PaddingConfig) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		if padding := fingerprint.GeneratePadding(cfg); len(padding) > 0 {
			headerName := fingerprint.PaddingHeaderName(cfg)
			req.SetHeader(headerName, hex.EncodeToString(padding))
		}
	})
}

// WithClientHelloSpecProvider constructs an [aoni.RequestModifier] assigning a dynamic uTLS spec provider.
func WithClientHelloSpecProvider(provider aoni.ClientHelloSpecProvider) aoni.RequestModifier {
	return Custom(func(req aoni.Request) {
		aoni.GetOrInitRequestConfig(req).ClientHelloSpecProvider = provider
	})
}
