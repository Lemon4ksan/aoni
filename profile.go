// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/fingerprint/profiles"
	internalProfile "github.com/lemon4ksan/aoni/internal/profile"
)

// ApplyTLSVariantToConfig maps uTLS ClientHello specifications, presets, and QUIC TLS parameters
// from a browser profile variant ([profiles.Variant]) into the target client configuration.
func ApplyTLSVariantToConfig(cfg *Config, variant *profiles.Variant) {
	if variant == nil {
		return
	}

	if cfg.Fingerprint.BrowserID == BrowserNone {
		if variant.HelloID.Client == "Firefox" {
			cfg.Fingerprint.BrowserID = BrowserFirefox
		} else {
			cfg.Fingerprint.BrowserID = BrowserChrome
		}

		cfg.Fingerprint.TLSClientHelloID = nil
	}

	if variant.HelloSpec != nil {
		cfg.Fingerprint.TLSClientHelloSpecProvider = internalProfile.StaticSpecProvider{Spec: variant.HelloSpec}
		cfg.Fingerprint.TLSClientHelloID = nil
	} else if variant.HelloID != (utls.ClientHelloID{}) {
		helloID := variant.HelloID
		cfg.Fingerprint.TLSClientHelloID = &helloID
		cfg.Fingerprint.TLSClientHelloSpecProvider = nil
	}

	if variant.HelloQUICSpec != nil {
		cfg.Fingerprint.TLSQUICClientHelloSpec = variant.HelloQUICSpec
	}
}

// ApplyHTTPVariantToConfig translates HTTP/2 SETTINGS frames, HTTP/3 QUIC transport limits,
// default browser request headers, and method-specific header ordering rules into the client configuration.
func ApplyHTTPVariantToConfig(cfg *Config, variant *profiles.Variant, os profiles.OSKey) {
	if variant == nil {
		return
	}

	h2Settings, h3Settings := internalProfile.ApplyHTTPSettings(variant)
	if h2Settings != nil {
		cfg.Fingerprint.H2Settings = h2Settings
	}

	cfg.Fingerprint.H3Settings = &h3Settings
	cfg.Defaults.Headers = internalProfile.PopulateHeaders(cfg.Defaults.Headers, variant, os)
	cfg.Fingerprint.HeaderOrder = internalProfile.BuildHeaderOrder(variant, os, "GET")
}

// ApplyProfileHeaders injects method-specific browser headers, WebKit/Gecko multipart boundary lines,
// and method-tailored header serialization sequences into the outgoing request contract.
func ApplyProfileHeaders(req Request, variant *profiles.Variant, os profiles.OSKey) {
	if variant == nil {
		return
	}

	if variant.InsertHeaders != nil {
		headersMap := make(map[string]string)
		if stdReq := req.HTTPRequest(); stdReq != nil {
			for k, v := range stdReq.Header {
				if len(v) > 0 {
					headersMap[k] = v[0]
				}
			}
		}

		variant.InsertHeaders(headersMap, req.Method())

		for k, v := range headersMap {
			if len(k) > 0 && k[0] == ':' {
				continue
			}

			if v != "" && req.Header(k) == "" {
				req.SetHeader(k, v)
			}
		}
	}

	if variant.BoundaryFunc != nil {
		cfg := GetOrInitRequestConfig(req)
		cfg.MultipartBoundary = variant.BoundaryFunc()
	}

	if variant.HeaderCache != nil {
		setOrderedHeaders(req, variant, os)
	}
}

func setOrderedHeaders(req Request, variant *profiles.Variant, os profiles.OSKey) {
	cfg := GetOrInitRequestConfig(req)
	cfg.OrderedHeaders = internalProfile.BuildHeaderOrder(variant, os, req.Method())
}
