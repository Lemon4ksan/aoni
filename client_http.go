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
)

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
