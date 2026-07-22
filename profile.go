// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license can be found in the LICENSE file.

package aoni

import (
	"net/http"

	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/fingerprint/h3"
	"github.com/lemon4ksan/aoni/fingerprint/profiles"
)

// ApplyTLSVariantToConfig translates and applies TLS fingerprint configurations
// from a browser profile variant to the client configuration.
func ApplyTLSVariantToConfig(cfg *Config, variant *profiles.Variant) {
	if cfg.Fingerprint.BrowserID == BrowserNone {
		if variant.HelloID.Client == "Firefox" {
			cfg.Fingerprint.BrowserID = BrowserFirefox
		} else {
			cfg.Fingerprint.BrowserID = BrowserChrome
		}

		cfg.Fingerprint.TLSClientHelloID = nil
	}

	if variant.HelloSpec != nil {
		cfg.Fingerprint.TLSClientHelloSpecProvider = staticSpecProvider{Spec: variant.HelloSpec}
		cfg.Fingerprint.TLSClientHelloID = nil
	} else if variant.HelloID != (utls.ClientHelloID{}) {
		helloID := variant.HelloID
		cfg.Fingerprint.TLSClientHelloID = &helloID
		cfg.Fingerprint.TLSClientHelloSpecProvider = nil
	}
}

// ApplyHTTPVariantToConfig maps HTTP/2, HTTP/3 transport frames and base browser headers
// from a browser profile variant to the client configuration.
func ApplyHTTPVariantToConfig(cfg *Config, variant *profiles.Variant, os profiles.OSKey) {
	h2Settings, h3Settings := applyHTTPSettings(variant)
	if h2Settings != nil {
		cfg.Fingerprint.H2Settings = h2Settings
	}

	cfg.Fingerprint.H3Settings = &h3Settings

	if variant.BuildHeaders != nil {
		if cfg.Defaults.Headers == nil {
			cfg.Defaults.Headers = make(http.Header)
		}

		for _, h := range variant.BuildHeaders(os) {
			if h.Value != "" {
				cfg.Defaults.Headers.Set(h.Name, h.Value)
			}
		}
	}

	if variant.HeaderCache == nil {
		return
	}

	enums := variant.HeaderCache.Enums(os.IsMobile())

	methodOrder, ok := enums["GET"]
	if !ok {
		return
	}

	getHeadersOrder := make([]string, len(methodOrder))
	for h, idx := range methodOrder {
		if idx >= 0 && idx < len(getHeadersOrder) {
			getHeadersOrder[idx] = h
		}
	}

	cfg.Fingerprint.HeaderOrder = getHeadersOrder
}

// ApplyProfileHeaders populates the target outgoing request with browser-grade headers,
// boundary lines, and frame order sequences matching the profile variant.
func ApplyProfileHeaders(req *http.Request, variant *profiles.Variant, os profiles.OSKey) {
	headersMap := make(map[string]string)
	for k, v := range req.Header {
		if len(v) > 0 {
			headersMap[k] = v[0]
		}
	}

	if variant.InsertHeaders != nil {
		variant.InsertHeaders(headersMap, req.Method)
	}

	for k, v := range headersMap {
		if v != "" && req.Header.Get(k) == "" {
			req.Header.Set(k, v)
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

func setOrderedHeaders(req *http.Request, variant *profiles.Variant, os profiles.OSKey) {
	enums := variant.HeaderCache.Enums(os.IsMobile())

	methodOrder, ok := enums[req.Method]
	if !ok {
		methodOrder = enums["GET"]
	}

	ordered := make([]string, len(methodOrder))
	for h, idx := range methodOrder {
		if idx >= 0 && idx < len(ordered) {
			ordered[idx] = h
		}
	}

	cfg := GetOrInitRequestConfig(req)
	cfg.OrderedHeaders = ordered
}

func applyHTTPSettings(variant *profiles.Variant) (http2 *h2.Settings, http3 h3.Settings) {
	if variant.ConfigureH2 != nil {
		var h2s profiles.H2Settings
		variant.ConfigureH2(&h2s)
		http2 = h2.SettingsFromProfile(h2s)
	}

	if variant.HelloID.Client == "Firefox" {
		http3 = h3.FirefoxSettings
	} else {
		http3 = h3.ChromeSettings
	}

	return http2, http3
}
