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

// WithSettings sets custom HTTP/2 SETTINGS frame parameters (header table size, max concurrent streams, initial window size).
//
// # RFC Compliance
//
// Conforms to RFC 9113 (HTTP/2) Section 6.5.
func WithSettings(settings h2.Settings) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H2Settings = &settings
	}
}

// WithH2FramedTransport is an alias for [WithSettings].
func WithH2FramedTransport(settings h2.Settings) aoni.ClientOption {
	return WithSettings(settings)
}

// WithProfileH2Settings extracts and sets HTTP/2 SETTINGS parameters from a [profiles.H2Settings] profile.
func WithProfileH2Settings(s profiles.H2Settings) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H2Settings = h2.SettingsFromProfile(s)
	}
}

// WithHTTP3Settings sets custom HTTP/3 QUIC connection and QPACK settings (RFC 9114).
func WithHTTP3Settings(settings h3.Settings) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H3Settings = &settings
	}
}

// WithH2ServerPush configures whether the HTTP/2 client accepts server-pushed streams (RFC 9113 §8.4).
//
// By default in modern browsers (Chrome/Firefox), SETTINGS_ENABLE_PUSH is disabled (0).
//
// # RFC Compliance
//
// Conforms to RFC 9113 (HTTP/2) Section 8.4.
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

// WithHTTP2Config configures low-level HTTP/2 connection parameters (ping timeouts, strict errors).
func WithHTTP2Config(cfg aoni.HTTP2Config) aoni.ClientOption {
	return func(c *aoni.Config) {
		c.Engine.HTTP2Config = &cfg
	}
}

// WithHTTP2Configurer registers a custom [fingerprint.HTTP2Configurer] to customize the HTTP/2 transport.
func WithHTTP2Configurer(configurer fingerprint.HTTP2Configurer) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint.H2Configurer = configurer
	}
}

// WithHTTP3 enables HTTP/3 over QUIC transport protocol (RFC 9114).
//
// # Example
//
//	client := aoni.NewClient(nil,
//	    option.WithHTTP3(),
//	)
//
// # RFC Compliance
//
// Conforms to RFC 9114 (HTTP/3) and RFC 9000 (QUIC: A UDP-Based Multiplexed and Secure Transport).
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
