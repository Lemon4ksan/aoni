// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"

	"github.com/lemon4ksan/aoni/cookie"
	"github.com/lemon4ksan/aoni/h2"
	"github.com/lemon4ksan/aoni/h3"
	"github.com/lemon4ksan/aoni/profiles"
)

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
		cfg.Fingerprint.TLSClientHelloSpecProvider = staticSpecProvider{Spec: variant.HelloSpec}
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

// HTTP2Config tunes the underlying http2.Transport connection parameters
// (such as idle pings and transport-level timeouts).
type HTTP2Config struct {
	// ReadIdleTimeout is the heavy-traffic keepalive ping timeout.
	// Sends a PING frame if the connection is idle (default: 0 = disabled).
	ReadIdleTimeout time.Duration
	// PingTimeout is the duration to wait for a PONG response before closing the connection.
	PingTimeout time.Duration
	// AllowHTTP allows HTTP/2 over plain, unencrypted TCP connections (h2c / Prior Knowledge).
	AllowHTTP bool
}

// WithHTTP2Config configures the low-level HTTP/2 connection parameters.
func WithHTTP2Config(cfg HTTP2Config) ClientOption {
	// For consistency with http3 we leave this option in the core module
	return func(c *Config) {
		c.Engine.HTTP2Config = &cfg
	}
}

// QUICMigrationConfig controls QUIC Connection Migration for HTTP/3.
type QUICMigrationConfig struct {
	// EnableMigration enables QUIC Connection Migration.
	EnableMigration bool
	// KeepAlivePeriod sends periodic keepalive packets to maintain the connection.
	KeepAlivePeriod time.Duration
	// MaxIdleTimeout is the maximum duration without network activity before connection close.
	MaxIdleTimeout time.Duration
	// DisablePathMTUDiscovery disables Path MTU Discovery during migration.
	DisablePathMTUDiscovery bool
	// InitialPacketSize sets the initial QUIC packet size.
	InitialPacketSize uint16
}

// DefaultQUICMigrationConfig returns a [QUICMigrationConfig] with production-ready defaults.
func DefaultQUICMigrationConfig() QUICMigrationConfig {
	return QUICMigrationConfig{
		EnableMigration:   true,
		KeepAlivePeriod:   15 * time.Second,
		MaxIdleTimeout:    30 * time.Second,
		InitialPacketSize: 1200,
	}
}

// WithHTTP3 returns a clone of c that sends requests over HTTP/3 (QUIC).
// Uses [DefaultQUICMigrationConfig] for migration settings.
func (c *Client) WithHTTP3() *Client {
	return c.WithHTTP3Config(nil)
}

// WithHTTP3Config returns a clone of c that sends requests over HTTP/3 (QUIC) with custom migration settings.
func (c *Client) WithHTTP3Config(config *QUICMigrationConfig) *Client {
	newClient := c.Clone()

	if config == nil {
		cfg := DefaultQUICMigrationConfig()
		config = &cfg
	}

	quicCfg := &quic.Config{
		EnableDatagrams:         true,
		DisablePathMTUDiscovery: config.DisablePathMTUDiscovery,
		InitialPacketSize:       config.InitialPacketSize,
	}

	if config.KeepAlivePeriod > 0 {
		quicCfg.KeepAlivePeriod = config.KeepAlivePeriod
	}

	if config.MaxIdleTimeout > 0 {
		quicCfg.MaxIdleTimeout = config.MaxIdleTimeout
	}

	if c.fingerprint.H3Settings != nil {
		quicCfg.InitialStreamReceiveWindow = c.fingerprint.H3Settings.InitialStreamReceiveWindow
		quicCfg.MaxStreamReceiveWindow = c.fingerprint.H3Settings.MaxStreamReceiveWindow
		quicCfg.InitialConnectionReceiveWindow = c.fingerprint.H3Settings.InitialConnectionReceiveWindow
		quicCfg.MaxConnectionReceiveWindow = c.fingerprint.H3Settings.MaxConnectionReceiveWindow
		quicCfg.MaxIncomingStreams = c.fingerprint.H3Settings.MaxIncomingStreams
		quicCfg.MaxIncomingUniStreams = c.fingerprint.H3Settings.MaxIncomingUniStreams
		quicCfg.EnableDatagrams = c.fingerprint.H3Settings.EnableDatagrams
	}

	rt := &http3.Transport{
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"h3"},
		},
		QUICConfig: quicCfg,
	}

	newClient.engine = &http.Client{
		Transport: rt,
	}

	return newClient
}

// staticSpecProvider wraps a static *utls.ClientHelloSpec as a ClientHelloSpecProvider.
type staticSpecProvider struct {
	Spec *utls.ClientHelloSpec
}

// ClientHelloSpec returns the underlying static spec.
func (s staticSpecProvider) ClientHelloSpec() (*utls.ClientHelloSpec, error) {
	return s.Spec, nil
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
			if cjTrans, ok := httpClient.Transport.(*cookie.Transport); ok {
				cjTrans.Next = framed
			} else {
				httpClient.Transport = framed
			}
		}
	}
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
		cfg := GetOrInitRequestConfig(req)
		cfg.MultipartBoundary = variant.BoundaryFunc()
	}

	if variant.HeaderCache != nil {
		setOrderedHeaders(req, variant, os)
	}
}
