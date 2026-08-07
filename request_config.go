// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import "github.com/lemon4ksan/aoni/internal/pipeline"

// RequestConfig aggregates request-scoped options and transport overrides.
type RequestConfig = pipeline.RequestConfig

// RedactConfigCtxKey is the context key used to store RedactConfig in the request context.
type RedactConfigCtxKey = pipeline.RedactConfigCtxKey

// JA4ReportStore acts as a shared carrier used by low-level TLS dialers to record
// computed JA4 signatures and propagate them to the active telemetry context.
type JA4ReportStore = pipeline.JA4ReportStore

// CacheKey uniquely identifies a cached HTTP request without string concatenations.
type CacheKey = pipeline.CacheKey

// CachedResponse holds a serialized HTTP response stored in cache backends.
type CachedResponse = pipeline.CachedResponse

// GetRequestConfig retrieves the RequestConfig instance attached to the context.
var GetRequestConfig = pipeline.GetRequestConfig

// GetOrInitRequestConfig retrieves or allocates a [RequestConfig] associated with the provided target.
var GetOrInitRequestConfig = pipeline.GetOrInitRequestConfig

// GetPipeline retrieves the request-specific PipelineConfig from context.
var GetPipeline = pipeline.GetPipeline

// AllocRequestConfig allocates a pooled [RequestConfig] and stores it in ctx, returning the
// enriched context and the config pointer.
var AllocRequestConfig = pipeline.AllocRequestConfig

// CloseResponse drains up to 2KB of unread body payload to preserve Keep-Alive sockets,
// closes the response body stream, and recycles request context resources.
var CloseResponse = pipeline.CloseResponse

// ApplyRequestConfigDefaults merges client-level defaults into uninitialized fields of [RequestConfig].
func ApplyRequestConfigDefaults(cfg *RequestConfig, c *Client) {
	if !cfg.SSRFGuard {
		cfg.SSRFGuard = c.network.SSRFGuard
	}

	if !cfg.ProxyDNS {
		cfg.ProxyDNS = c.network.ProxyDNS
	}

	if !cfg.MultiReadDisableDisk {
		cfg.MultiReadDisableDisk = c.defaults.MultiReadDisableDisk
	}

	if cfg.HappyEyeballsDelay == 0 {
		cfg.HappyEyeballsDelay = c.network.HappyEyeballsDelay
	}

	if cfg.MultiReadThreshold == 0 {
		cfg.MultiReadThreshold = c.defaults.MultiReadThreshold
	}

	if cfg.ProxyAddr == nil {
		cfg.ProxyAddr = c.network.ProxyAddr
	}

	if cfg.P0fSignature == nil {
		cfg.P0fSignature = c.fingerprint.P0fSignature
	}

	if cfg.SessionCache == nil {
		cfg.SessionCache = c.fingerprint.SessionCache
	}

	if cfg.PacketPadding == nil {
		cfg.PacketPadding = c.fingerprint.PacketPadding
	}

	if cfg.SocketController == nil {
		cfg.SocketController = c.network.SocketController
	}

	if cfg.ClientHelloSpecProvider == nil {
		cfg.ClientHelloSpecProvider = c.fingerprint.TLSClientHelloSpecProvider
	}

	if cfg.JA4Callback == nil {
		cfg.JA4Callback = c.fingerprint.JA4Callback
	}

	if cfg.QueryEncoder == nil && c.defaults.QueryEncoder != nil {
		// QueryEncoder has identical underlying type in both packages: func(any) (url.Values, error)
		cfg.QueryEncoder = pipeline.QueryEncoder(c.defaults.QueryEncoder)
	}

	if len(c.defaults.Decoders) > 0 {
		if cfg.Decoders == nil {
			cfg.Decoders = make(map[string]pipeline.ResponseDecoder, len(c.defaults.Decoders))
		}

		for k, v := range c.defaults.Decoders {
			if _, ok := cfg.Decoders[k]; !ok {
				cfg.Decoders[k] = v
			}
		}
	}

	if len(c.fingerprint.CertificatePins) > 0 {
		c.mergeCertificatePins(cfg)
	}
}

func (c *Client) mergeCertificatePins(cfg *RequestConfig) {
	for domain, hashes := range c.fingerprint.CertificatePins {
		for _, h := range hashes {
			if cfg.CertificatePins == nil {
				cfg.CertificatePins = make(map[string][]string)
			}

			hasHash := false
			for _, existing := range cfg.CertificatePins[domain] {
				if existing == h {
					hasHash = true
					break
				}
			}

			if !hasHash {
				cfg.CertificatePins[domain] = append(cfg.CertificatePins[domain], h)
			}
		}
	}
}
