// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/lemon4ksan/foundation/silicon/sysnet"
	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/internal/transport"
)

// Dial establishes an unencrypted L4 TCP socket connection to addr using a default 15-second timeout.
// It acts as a convenience wrapper around DialContext using context.Background.
// Safe for concurrent use across multiple goroutines.
func (c *Client) Dial(addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return c.DialContext(ctx, "tcp", addr)
}

// DialContext establishes a raw L4 TCP socket connection applying active proxy tunneling,
// DNS resolution, Happy Eyeballs v2 dual-stack racing (RFC 8305), p0f OS stack spoofing,
// SSRF IP guards, and pre-dial TCP delay jitter.
//
// Pipeline Stages Executed:
//  1. Pre-dial TCP jitter delay (ApplyTCPDelay).
//  2. Static Host rewrite rules (HostRewriteRules).
//  3. Happy Eyeballs v2 IPv4/IPv6 dual-stack resolution and racing.
//  4. SSRF private/loopback/link-local IP filtering (if SSRFGuard is enabled).
//  5. OS kernel socket option configuration (SO_MARK, TCP_MAXSEG, SO_BUSY_POLL, p0f).
//  6. SOCKS5 / SOCKS5h / HTTP CONNECT proxy tunneling (if ProxyURL is configured).
//  7. TCP payload write-chunking fragmentation (if FragmentConfig is active).
//
// Safe for concurrent use across multiple goroutines.
func (c *Client) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := transport.NewUniversalDialer()
	dialCfg := c.buildDialConfig(ctx)

	conn, err := dialer.DialContext(ctx, network, addr, dialCfg)
	if err == nil {
		sysnet.TuneSocketConnWithFlags(conn, uint64(c.cfg.Network.ExperimentalFlags))
	}

	return conn, err
}

// DialTLS establishes an encrypted L7 TLS or uTLS connection over L4 TCP.
// It negotiates ALPN protocols ("h2", "http/1.1"), applies browser ClientHello emulation
// (Chrome, Firefox, Safari), performs Encrypted Client Hello (ECH, RFC 9484) resolution,
// supports 0-RTT Early Data (RFC 8446/9001), and validates SPKI certificate pins.
//
// Fingerprint Precedence:
// If multiple TLS fingerprint options are configured, transport.UniversalDialer applies them
// in the following order:
//  1. ClientHelloSpecProvider (custom full spec builder)
//  2. TLSClientHelloID (explicit uTLS profile preset)
//  3. BrowserID (predefined high-level browser profile)
//
// Safe for concurrent use across multiple goroutines.
func (c *Client) DialTLS(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := transport.NewUniversalDialer()
	dialCfg := c.buildDialConfig(ctx)

	conn, err := dialer.DialTLSContext(ctx, network, addr, dialCfg)
	if err == nil {
		sysnet.TuneSocketConn(conn)
	}

	return conn, err
}

// DialTLSForWS establishes an encrypted TLS socket connection tailored for WebSocket upgrades.
// If the underlying http.Transport has a custom DialTLSContext registered, it delegates to that
// function to maintain transport consistency; otherwise, it delegates to DialTLSContext.
// Safe for concurrent use across multiple goroutines.
func (c *Client) DialTLSForWS(ctx context.Context, addr string) (net.Conn, error) {
	if tr := c.Transport(); tr != nil && tr.DialTLSContext != nil {
		return tr.DialTLSContext(ctx, "tcp", addr)
	}

	return c.DialTLS(ctx, "tcp", addr)
}

// DialPlainForWS establishes an unencrypted raw TCP socket connection for WebSocket upgrades,
// applying active proxy routing, SSRF guards, and TCP write-chunking fragmentation.
// If the underlying http.Transport has a custom DialContext registered, it delegates to that
// function before applying WebSocket payload fragmentation wrappers.
// Safe for concurrent use across multiple goroutines.
func (c *Client) DialPlainForWS(ctx context.Context, addr string) (net.Conn, error) {
	if tr := c.Transport(); tr != nil && tr.DialContext != nil {
		conn, err := tr.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}

		return c.applyWSFragmentation(ctx, conn), nil
	}

	conn, err := c.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	return c.applyWSFragmentation(ctx, conn), nil
}

// applyWSFragmentation decorates a raw socket with TCP payload write-chunking if configured.
func (c *Client) applyWSFragmentation(ctx context.Context, conn net.Conn) net.Conn {
	if cfg := GetRequestConfig(ctx); cfg != nil && cfg.Fragment != nil {
		return transport.ApplyFragmentation(conn, *cfg.Fragment)
	}

	return conn
}

// applyDialers binds the client's dialing functions (DialContext, DialTLSContext)
// to a standard http.Transport instance, injecting aoni's transport features into stdlib clients.
func (c *Client) applyDialers(tr *http.Transport) {
	if tr == nil {
		return
	}

	tr.DialContext = c.DialContext
	tr.DialTLSContext = c.DialTLS
}

// buildDialConfig constructs a self-contained transport.DialConfig DTO by merging
// client-level defaults with per-request context overrides extracted via GetRequestConfig.
func (c *Client) buildDialConfig(ctx context.Context) transport.DialConfig {
	cfgDTO := c.cfg.Clone()
	cfg := cfgDTO.BuildDialConfig(ctx)
	cfg.InterfaceName = c.cfg.Network.InterfaceName
	cfg.SocketMark = c.cfg.Network.SocketMark
	cfg.BaseTLSConfig = c.resolveBaseTLSConfig(ctx)
	cfg.HelloID = c.resolveHelloID()
	cfg.ApplyRequestOverrides(GetRequestConfig(ctx))

	return cfg
}

// resolveBaseTLSConfig extracts the base tls.Config from the underlying http.Transport
// and applies per-request TLS overrides (e.g. InsecureSkipVerify) from ctx.
func (c *Client) resolveBaseTLSConfig(ctx context.Context) *tls.Config {
	var base *tls.Config
	if tr := c.Transport(); tr != nil {
		base = tr.TLSClientConfig
	}

	return TLSConfigWithOverride(ctx, base)
}

// resolveHelloID maps the active BrowserID preset (Chrome, Firefox, Safari) to its
// corresponding uTLS HelloID auto-preset, or returns the explicitly set TLSClientHelloID.
func (c *Client) resolveHelloID() *utls.ClientHelloID {
	f := c.cfg.Fingerprint
	if f.TLSClientHelloID != nil {
		return f.TLSClientHelloID
	}

	switch f.BrowserID {
	case BrowserFirefox:
		return &utls.HelloFirefox_Auto
	case BrowserSafari:
		return &utls.HelloSafari_Auto
	default:
		return &utls.HelloChrome_Auto
	}
}
