// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"net/http"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/h2"
	"github.com/lemon4ksan/aoni/h3"
	"github.com/lemon4ksan/aoni/profiles"
)

// StaticSpecProvider wraps a static *utls.ClientHelloSpec as a ClientHelloSpecProvider.
type StaticSpecProvider struct {
	Spec *utls.ClientHelloSpec
}

// ClientHelloSpec returns the underlying static spec.
func (s StaticSpecProvider) ClientHelloSpec() (*utls.ClientHelloSpec, error) {
	return s.Spec, nil
}

func getOrInitRequestConfig(req *http.Request) *RequestConfig {
	cfg := GetRequestConfig(req.Context())
	if cfg == nil {
		cfg = &RequestConfig{
			Metadata: make(map[string]any),
		}
		ctx := context.WithValue(req.Context(), requestConfigKey{}, cfg)
		*req = *req.WithContext(ctx)
	}

	return cfg
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

	cfg := getOrInitRequestConfig(req)
	cfg.OrderedHeaders = ordered
}

func (c *Client) reapplyH2Settings(tr *http.Transport) {
	if tr == nil {
		return
	}

	if c.fingerprint.H2Configurer != nil {
		t2, err := http2.ConfigureTransports(tr)
		if err == nil && t2 != nil {
			t2.TLSClientConfig = tr.TLSClientConfig
			_ = c.fingerprint.H2Configurer.ConfigureHTTP2(t2)
		}
	}

	if c.fingerprint.H2Settings != nil {
		framed := h2.NewFramedTransport(tr, *c.fingerprint.H2Settings)
		if httpClient, ok := c.engine.(*http.Client); ok {
			httpClient.Transport = framed
		}
	}
}

// ApplyTLSVariantToConfig applies TLS fingerprint settings from a browser profile variant to a Config.
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
		cfg.Fingerprint.TLSClientHelloSpecProvider = StaticSpecProvider{Spec: variant.HelloSpec}
		cfg.Fingerprint.TLSClientHelloID = nil
	} else if variant.HelloID != (utls.ClientHelloID{}) {
		helloID := variant.HelloID
		cfg.Fingerprint.TLSClientHelloID = &helloID
		cfg.Fingerprint.TLSClientHelloSpecProvider = nil
	}
}

// ApplyHTTPVariantToConfig applies HTTP/2 and HTTP/3 settings and default headers from a variant to a Config.
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

	var getHeadersOrder []string

	if variant.HeaderCache == nil {
		return
	}

	enums := variant.HeaderCache.Enums(os.IsMobile())
	methodOrder, ok := enums["GET"]

	if !ok {
		return
	}

	getHeadersOrder = make([]string, len(methodOrder))
	for h, idx := range methodOrder {
		if idx >= 0 && idx < len(getHeadersOrder) {
			getHeadersOrder[idx] = h
		}
	}

	cfg.Fingerprint.HeaderOrder = getHeadersOrder
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

// ApplyProfileHeaders injects profile-specific headers and boundaries into an HTTP request.
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
		if v == "" {
			continue
		}

		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}

	if variant.BoundaryFunc != nil {
		cfg := getOrInitRequestConfig(req)
		cfg.MultipartBoundary = variant.BoundaryFunc()
	}

	if variant.HeaderCache != nil {
		setOrderedHeaders(req, variant, os)
	}
}
