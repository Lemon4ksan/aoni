// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package option provides functional options for customizing an [aoni.Client] configuration.
//
// Options are consumed by [aoni.NewClient] or [aoni.Client.With] to configure global client defaults,
// such as base URLs, timeouts, proxy rotators, TLS fingerprints, and execution pipeline behavior.
//
// Thread Safety:
// All options operate immutably on [aoni.Config] structures, preserving thread safety and concurrent client reuse.
package option

import (
	"net/http"
	"strings"

	"github.com/lemon4ksan/aoni"
)

// Option is an alias for [aoni.ClientOption].
type Option = aoni.ClientOption

// WithConfig returns an [aoni.ClientOption] that replaces the entire client configuration at once.
func WithConfig(cfg aoni.Config) aoni.ClientOption {
	return func(c *aoni.Config) {
		c.Network = cfg.Network.Clone()
		c.Fingerprint = cfg.Fingerprint.Clone()
		c.Defaults = cfg.Defaults.Clone()
		c.Engine = cfg.Engine
	}
}

// WithDefaultsBlock returns an [aoni.ClientOption] replacing only the [aoni.ClientDefaults] configuration layer.
func WithDefaultsBlock(defaults aoni.ClientDefaults) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults = defaults.Clone()
	}
}

// WithNetworkBlock returns an [aoni.ClientOption] replacing only the [aoni.NetworkConfig] configuration layer.
func WithNetworkBlock(network aoni.NetworkConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Network = network.Clone()
	}
}

// WithFingerprintBlock returns an [aoni.ClientOption] replacing only the [aoni.FingerprintConfig] configuration layer.
func WithFingerprintBlock(fingerprint aoni.FingerprintConfig) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Fingerprint = fingerprint.Clone()
	}
}

// WithBaremetal switches the client to maximum-speed ("bare-metal") mode:
// it disables background rotation, HTML tag validation, copying of default headers, and unnecessary wrappers.
func WithBaremetal() aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Defaults.Pipeline.Decompress = false
		cfg.Defaults.Pipeline.Validate = false
		cfg.Defaults.Pipeline.Challenge = false
		cfg.Defaults.MaxResponseSize = -1
		cfg.Defaults.MultiReadThreshold = -1
		cfg.Defaults.RefererAutomaton = false
		cfg.Defaults.Headers = nil
	}
}

// WithEngine returns an [aoni.ClientOption] replacing the underlying [aoni.HTTPDoer] engine (e.g. custom [*http.Client]).
func WithEngine(engine aoni.HTTPDoer) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		cfg.Engine.CustomEngine = engine
	}
}

// WithProtocol registers a custom [http.RoundTripper] handler for non-HTTP schemes (e.g. aoni.ProtocolFile, aoni.ProtocolS3, or raw string).
func WithProtocol[T ~string](scheme T, handler http.RoundTripper) aoni.ClientOption {
	return func(cfg *aoni.Config) {
		if cfg.Engine.Protocols == nil {
			cfg.Engine.Protocols = make(aoni.ProtocolMap)
		}

		normProto := aoni.Protocol(strings.ToLower(strings.TrimSpace(string(scheme))))
		if normProto != "" && handler != nil {
			cfg.Engine.Protocols[normProto] = handler
		}
	}
}
