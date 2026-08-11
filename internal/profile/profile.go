// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package profile

import (
	"net/http"

	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/fingerprint/h3"
	"github.com/lemon4ksan/aoni/fingerprint/profiles"
)

// StaticSpecProvider wraps a static *utls.ClientHelloSpec.
type StaticSpecProvider struct {
	Spec *utls.ClientHelloSpec
}

func (s StaticSpecProvider) ClientHelloSpec() (*utls.ClientHelloSpec, error) {
	return s.Spec, nil
}

// ApplyHTTPSettings extracts HTTP/2 and HTTP/3 settings from a profile variant.
func ApplyHTTPSettings(variant *profiles.Variant) (h2Settings *h2.Settings, h3Settings h3.Settings) {
	if variant.ConfigureH2 != nil {
		var h2s profiles.H2Settings
		variant.ConfigureH2(&h2s)
		h2Settings = h2.SettingsFromProfile(h2s)
	}

	if variant.HelloID.Client == "Firefox" {
		h3Settings = h3.FirefoxSettings
	} else {
		h3Settings = h3.ChromeSettings
	}

	return h2Settings, h3Settings
}

// BuildHeaderOrder returns ordered header keys for a given method from variant header cache.
func BuildHeaderOrder(variant *profiles.Variant, os profiles.OSKey, method string) []string {
	if variant == nil || variant.HeaderCache == nil {
		return nil
	}

	enums := variant.HeaderCache.Enums(os.IsMobile())

	methodOrder, ok := enums[method]
	if !ok {
		methodOrder = enums["GET"]
	}

	ordered := make([]string, len(methodOrder))
	for h, idx := range methodOrder {
		if idx >= 0 && idx < len(ordered) {
			ordered[idx] = h
		}
	}

	return ordered
}

// PopulateHeaders applies headers defined by variant build headers.
func PopulateHeaders(target http.Header, variant *profiles.Variant, os profiles.OSKey) http.Header {
	if variant == nil || variant.BuildHeaders == nil {
		return target
	}

	if target == nil {
		target = make(http.Header)
	}

	for _, h := range variant.BuildHeaders(os) {
		if h.Value != "" {
			target.Set(h.Name, h.Value)
		}
	}

	return target
}
