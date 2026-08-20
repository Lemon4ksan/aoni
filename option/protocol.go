// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package option

import (
	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fingerprint"
	"github.com/lemon4ksan/aoni/fingerprint/h2"
	"github.com/lemon4ksan/aoni/fingerprint/h3"
	"github.com/lemon4ksan/aoni/fingerprint/profiles"
)

// WithSettings returns an [aoni.ClientOption] setting custom HTTP/2 SETTINGS frame parameters.
func WithSettings(settings h2.Settings) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H2Settings = &settings
	}
}

// WithH2FramedTransport is an alias for [WithSettings].
func WithH2FramedTransport(settings h2.Settings) aoni.ClientOption {
	return WithSettings(settings)
}

// WithProfileH2Settings returns an [aoni.ClientOption] extracting HTTP/2 transport parameters from a [profiles.H2Settings].
func WithProfileH2Settings(s profiles.H2Settings) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H2Settings = h2.SettingsFromProfile(s)
	}
}

// WithHTTP3Settings returns an [aoni.ClientOption] setting custom HTTP/3 QUIC connection parameters.
func WithHTTP3Settings(settings h3.Settings) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H3Settings = &settings
	}
}

// WithH2ServerPush configures whether the HTTP/2 client accepts server-pushed resources (RFC 9113 §8.4).
func WithH2ServerPush(enable bool) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		var s h2.Settings
		if cfg.Fingerprint.H2Settings != nil {
			s = *cfg.Fingerprint.H2Settings
		} else {
			s = h2.ChromeSettings
		}

		if enable {
			s.EnablePush = 1
		} else {
			s.EnablePush = 0
		}

		cfg.Fingerprint.H2Settings = &s
	}
}

// WithHTTP2Config returns an [aoni.ClientOption] configuring low-level HTTP/2 connection parameters.
func WithHTTP2Config(cfg aoni.HTTP2Config) aoni.ClientOption {
	return func(c *aoni.Config) {
		c.Engine.HTTP2Config = &cfg
	}
}

// WithHTTP2Configurer returns an [aoni.ClientOption] configuring an [fingerprint.HTTP2Configurer] interface on the transport.
func WithHTTP2Configurer(configurer fingerprint.HTTP2Configurer) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H2Configurer = configurer
	}
}

// WithHTTP3 returns an [aoni.ClientOption] enabling HTTP/3 over QUIC (RFC 9114).
func WithHTTP3(settings ...h3.Settings) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if len(settings) > 0 {
			s := settings[0]
			cfg.Fingerprint.H3Settings = &s
		} else {
			s := h3.ChromeSettings
			cfg.Fingerprint.H3Settings = &s
		}
	}
}
